package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/policy"
	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

var (
	ErrModeNotFound       = errors.New("场景不存在")
	ErrModeDisabled       = errors.New("场景已停用，无法执行")
	ErrRunNotFound        = errors.New("执行记录不存在")
	ErrModeAlreadyRunning = errors.New("场景正在执行中，当前配置为忽略重复触发")
	ErrRunModeUnsupported = errors.New("当前版本暂不支持该并发模式（仅支持 restart 与 single）")
)

const (
	DefaultMaxRunDuration = 5 * time.Minute

	GuardNodeID = "__guard__"

	RunStatusRunning   = "running"
	RunStatusOK        = "ok"
	RunStatusFailed    = "failed"
	RunStatusCancelled = "cancelled"

	StepStatusPending = "pending"
	StepStatusRunning = "running"
	StepStatusOK      = "ok"
	StepStatusFailed  = "failed"
	StepStatusSkipped = "skipped"
)

// RunStepMessage 是经由 WS 广播的单个步骤进度事件，与 04-详细设计说明书 §3.2 对应。
type RunStepMessage struct {
	RunID      uuid.UUID
	NodeID     string
	Path       string
	Alias      string
	Status     string
	Error      string
	Note       string
	RunStatus  string
	StartedAt  *time.Time
	FinishedAt *time.Time
}

type RunStepBroadcaster interface {
	BroadcastRunStep(RunStepMessage)
}

type ModeCondition struct {
	ItemID     uuid.UUID `json:"itemId"`
	Capability string    `json:"capability"`
	Op         string    `json:"op"`
	Value      any       `json:"value"`
}

type ModeGuard struct {
	All []ModeCondition `json:"all,omitempty"`
	Any []ModeCondition `json:"any,omitempty"`
}

type ModeDefaults struct {
	ContinueOnError bool `json:"continueOnError"`
}

type ModeNode struct {
	NodeID          string     `json:"nodeId"`
	Type            string     `json:"type"`
	Alias           string     `json:"alias,omitempty"`
	Enabled         *bool      `json:"enabled,omitempty"`
	ContinueOnError *bool      `json:"continueOnError,omitempty"`
	ItemID          *uuid.UUID `json:"itemId,omitempty"`
	Capability      string     `json:"capability,omitempty"`
	Value           any        `json:"value,omitempty"`
	ValueFrom       string     `json:"valueFrom,omitempty"`
	Ms              int        `json:"ms,omitempty"`
	When            *ModeGuard `json:"when,omitempty"`
	Then            []ModeNode `json:"then,omitempty"`
	Else            []ModeNode `json:"else,omitempty"`
}

type ModeDefinition struct {
	Version  int           `json:"version"`
	Guard    *ModeGuard    `json:"guard,omitempty"`
	Defaults *ModeDefaults `json:"defaults,omitempty"`
	Steps    []ModeNode    `json:"steps"`
}

type RunDetail struct {
	Run    storegen.Run
	Steps  []storegen.RunStep
	Reason string
}

type activeRun struct {
	modeID uuid.UUID
	cancel context.CancelFunc
	done   chan struct{}
}

type stackFrame struct {
	node ModeNode
	path string
}

// ModeRunner 负责场景执行生命周期：守卫校验、建 run、DFS 显式栈幂等推进、超时与并发控制。
type ModeRunner struct {
	pool           *pgxpool.Pool
	commands       CommandHandler
	broadcaster    RunStepBroadcaster
	simEnv         *SimEnvStore
	yields         *YieldStore
	maxRunDuration time.Duration

	mu           sync.Mutex
	active       map[uuid.UUID]*activeRun
	activeByMode map[uuid.UUID]uuid.UUID
	advanceLock  map[uuid.UUID]*sync.Mutex
	modeLock     map[uuid.UUID]*sync.Mutex
}

func NewModeRunner(pool *pgxpool.Pool, commands CommandHandler, broadcaster RunStepBroadcaster) *ModeRunner {
	return &ModeRunner{
		pool:           pool,
		commands:       commands,
		broadcaster:    broadcaster,
		simEnv:         NewSimEnvStore(),
		maxRunDuration: DefaultMaxRunDuration,
		active:         make(map[uuid.UUID]*activeRun),
		activeByMode:   make(map[uuid.UUID]uuid.UUID),
		advanceLock:    make(map[uuid.UUID]*sync.Mutex),
		modeLock:       make(map[uuid.UUID]*sync.Mutex),
	}
}

func (r *ModeRunner) SetSimEnv(env *SimEnvStore) {
	if env != nil {
		r.simEnv = env
	}
}

func (r *ModeRunner) SetYields(store *YieldStore) {
	r.yields = store
}

// SetMaxRunDuration 允许测试注入短超时，默认 5 分钟。
func (r *ModeRunner) SetMaxRunDuration(d time.Duration) {
	if d > 0 {
		r.maxRunDuration = d
	}
}

func (r *ModeRunner) lockForRun(runID uuid.UUID) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.advanceLock[runID]
	if !ok {
		m = &sync.Mutex{}
		r.advanceLock[runID] = m
	}
	return m
}

func (r *ModeRunner) lockForMode(modeID uuid.UUID) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()
	m, ok := r.modeLock[modeID]
	if !ok {
		m = &sync.Mutex{}
		r.modeLock[modeID] = m
	}
	return m
}

func (r *ModeRunner) ListModes(ctx context.Context) ([]storegen.Mode, error) {
	return storegen.New(r.pool).ListModes(ctx)
}

func (r *ModeRunner) GetMode(ctx context.Context, id uuid.UUID) (storegen.Mode, error) {
	m, err := storegen.New(r.pool).GetMode(ctx, pgUUID(id))
	if errors.Is(err, pgx.ErrNoRows) {
		return storegen.Mode{}, ErrModeNotFound
	}
	return m, err
}

type ModeWriteInput struct {
	Name       string
	Definition json.RawMessage
	RunMode    string
	Enabled    bool
	CreatedBy  uuid.UUID
}

func normalizeModeWriteInput(in ModeWriteInput) (ModeWriteInput, error) {
	in.Name = strings.TrimSpace(in.Name)
	if in.Name == "" || len([]rune(in.Name)) > 64 {
		return in, fmt.Errorf("%w: 场景名称不能为空且不超过 64 字", ErrModeValidation)
	}
	if in.RunMode == "" {
		in.RunMode = "restart"
	}
	switch in.RunMode {
	case "single", "restart", "queued", "parallel":
	default:
		return in, fmt.Errorf("%w: 不支持的 runMode %q", ErrModeValidation, in.RunMode)
	}
	return in, nil
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

func (r *ModeRunner) CreateMode(ctx context.Context, in ModeWriteInput) (storegen.Mode, error) {
	in, err := normalizeModeWriteInput(in)
	if err != nil {
		return storegen.Mode{}, err
	}
	if err := ValidateModeDefinition(ctx, r.pool, in.Definition); err != nil {
		return storegen.Mode{}, err
	}
	m, err := storegen.New(r.pool).InsertMode(ctx, storegen.InsertModeParams{
		Name:       in.Name,
		Definition: in.Definition,
		RunMode:    in.RunMode,
		Enabled:    in.Enabled,
		CreatedBy:  pgUUID(in.CreatedBy),
		CreatedAt:  pgTime(time.Now()),
	})
	if err != nil {
		if isUniqueViolation(err) {
			return storegen.Mode{}, ErrModeNameConflict
		}
		return storegen.Mode{}, err
	}
	return m, nil
}

func (r *ModeRunner) UpdateMode(ctx context.Context, id uuid.UUID, in ModeWriteInput) (storegen.Mode, error) {
	in, err := normalizeModeWriteInput(in)
	if err != nil {
		return storegen.Mode{}, err
	}
	if err := ValidateModeDefinition(ctx, r.pool, in.Definition); err != nil {
		return storegen.Mode{}, err
	}
	m, err := storegen.New(r.pool).UpdateMode(ctx, storegen.UpdateModeParams{
		ID:         pgUUID(id),
		Name:       in.Name,
		Definition: in.Definition,
		RunMode:    in.RunMode,
		Enabled:    in.Enabled,
		UpdatedAt:  pgTime(time.Now()),
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return storegen.Mode{}, ErrModeNotFound
		}
		if isUniqueViolation(err) {
			return storegen.Mode{}, ErrModeNameConflict
		}
		return storegen.Mode{}, err
	}
	return m, nil
}

func (r *ModeRunner) DeleteMode(ctx context.Context, id uuid.UUID) error {
	r.mu.Lock()
	prevRunID, hasActive := r.activeByMode[id]
	r.mu.Unlock()
	if hasActive {
		_, _ = r.Cancel(ctx, prevRunID)
	}
	rows, err := storegen.New(r.pool).DeleteMode(ctx, pgUUID(id))
	if err != nil {
		return err
	}
	if rows == 0 {
		return ErrModeNotFound
	}
	return nil
}

func (r *ModeRunner) GetRun(ctx context.Context, runID uuid.UUID) (RunDetail, error) {
	q := storegen.New(r.pool)
	run, err := q.GetRun(ctx, pgUUID(runID))
	if errors.Is(err, pgx.ErrNoRows) {
		return RunDetail{}, ErrRunNotFound
	}
	if err != nil {
		return RunDetail{}, err
	}
	steps, err := q.ListRunSteps(ctx, pgUUID(runID))
	if err != nil {
		return RunDetail{}, err
	}
	return RunDetail{Run: run, Steps: steps, Reason: deriveRunReason(run.Status, steps)}, nil
}

func deriveRunReason(status string, steps []storegen.RunStep) string {
	for _, st := range steps {
		if st.NodeID == GuardNodeID && st.Error.Valid && st.Error.String != "" {
			return st.Error.String
		}
	}
	if status == RunStatusFailed {
		for _, st := range steps {
			if st.Status == StepStatusFailed && st.Error.Valid && st.Error.String != "" {
				return st.Error.String
			}
		}
	}
	return ""
}

// Start 校验并发模式与顶层守卫，创建一条 run 并在后台异步推进。
func (r *ModeRunner) Start(ctx context.Context, modeID uuid.UUID, triggeredBy uuid.UUID, source string) (RunDetail, error) {
	if source == "" {
		source = "manual"
	}
	q := storegen.New(r.pool)
	mode, err := q.GetMode(ctx, pgUUID(modeID))
	if errors.Is(err, pgx.ErrNoRows) {
		return RunDetail{}, ErrModeNotFound
	}
	if err != nil {
		return RunDetail{}, err
	}

	if !mode.Enabled {
		return RunDetail{}, ErrModeDisabled
	}
	if mode.RunMode != "restart" && mode.RunMode != "single" {
		return RunDetail{}, ErrRunModeUnsupported
	}

	var def ModeDefinition
	if err := json.Unmarshal(mode.Definition, &def); err != nil {
		return RunDetail{}, fmt.Errorf("解析场景定义失败: %w", err)
	}

	// 顶层守卫校验：不满足时不执行任何动作，记录 cancelled 与原因并返回。
	if def.Guard != nil {
		ok, reason := r.evaluateGuard(ctx, def.Guard)
		if !ok {
			return r.recordGuardRejectedRun(ctx, modeID, mode.Definition, triggeredBy, source, reason)
		}
	}

	// 并发控制（按 modeID 串行化 Start，消除 single/restart 的 TOCTOU 竞态）
	modeMu := r.lockForMode(modeID)
	modeMu.Lock()
	defer modeMu.Unlock()

	r.mu.Lock()
	prevRunID, hasActive := r.activeByMode[modeID]
	r.mu.Unlock()
	if hasActive {
		if mode.RunMode == "single" {
			return RunDetail{}, ErrModeAlreadyRunning
		}
		// restart：取消旧执行并等待其落库收尾
		_, _ = r.Cancel(ctx, prevRunID)
	}

	// FR-5.9：确认可以启动执行后，再清除本次涉及（且未禁用）设备/能力的手动接管让位标记。
	if r.yields != nil {
		r.yields.ClearMany(collectModeYieldKeys(def.Steps))
	}

	runID := uuid.New()
	now := time.Now()
	run, err := q.InsertRun(ctx, storegen.InsertRunParams{
		ID:                 pgUUID(runID),
		ModeID:             pgUUID(modeID),
		Status:             RunStatusRunning,
		DefinitionSnapshot: mode.Definition,
		TriggeredBy:        pgUUID(triggeredBy),
		Source:             source,
		StartedAt:          pgTime(now),
	})
	if err != nil {
		return RunDetail{}, err
	}

	steps := make([]storegen.RunStep, 0, len(def.Steps))
	for i, node := range def.Steps {
		path := fmt.Sprintf("%03d", i)
		st, err := q.UpsertRunStep(ctx, storegen.UpsertRunStepParams{
			RunID:  pgUUID(runID),
			NodeID: node.NodeID,
			Path:   path,
			Alias:  pgText(node.Alias),
			Status: StepStatusPending,
		})
		if err != nil {
			return RunDetail{}, err
		}
		steps = append(steps, st)
	}

	runCtx, cancel := context.WithTimeout(context.Background(), r.maxRunDuration)
	ar := &activeRun{modeID: modeID, cancel: cancel, done: make(chan struct{})}
	r.mu.Lock()
	r.active[runID] = ar
	r.activeByMode[modeID] = runID
	r.mu.Unlock()

	go func() {
		defer func() {
			cancel()
			r.mu.Lock()
			delete(r.active, runID)
			if r.activeByMode[modeID] == runID {
				delete(r.activeByMode, modeID)
			}
			delete(r.advanceLock, runID)
			r.mu.Unlock()
			close(ar.done)
		}()
		_ = r.Advance(runCtx, runID)
	}()

	return RunDetail{Run: run, Steps: steps}, nil
}

func (r *ModeRunner) recordGuardRejectedRun(
	ctx context.Context,
	modeID uuid.UUID,
	definition json.RawMessage,
	triggeredBy uuid.UUID,
	source string,
	reason string,
) (RunDetail, error) {
	q := storegen.New(r.pool)
	runID := uuid.New()
	now := time.Now()
	_, err := q.InsertRun(ctx, storegen.InsertRunParams{
		ID:                 pgUUID(runID),
		ModeID:             pgUUID(modeID),
		Status:             RunStatusRunning,
		DefinitionSnapshot: definition,
		TriggeredBy:        pgUUID(triggeredBy),
		Source:             source,
		StartedAt:          pgTime(now),
	})
	if err != nil {
		return RunDetail{}, err
	}
	if err := q.CompleteRun(ctx, storegen.CompleteRunParams{
		ID:         pgUUID(runID),
		Status:     RunStatusCancelled,
		FinishedAt: pgTime(now),
	}); err != nil {
		return RunDetail{}, err
	}
	if err := r.saveAndBroadcast(ctx, RunStepMessage{
		RunID:      runID,
		NodeID:     GuardNodeID,
		Path:       "000",
		Alias:      "守卫条件检查",
		Status:     StepStatusSkipped,
		Error:      reason,
		RunStatus:  RunStatusCancelled,
		StartedAt:  &now,
		FinishedAt: &now,
	}); err != nil {
		return RunDetail{}, err
	}
	return r.GetRun(ctx, runID)
}

// Cancel 取消正在运行的执行，等待收尾落库后返回最新详情。
func (r *ModeRunner) Cancel(ctx context.Context, runID uuid.UUID) (RunDetail, error) {
	r.mu.Lock()
	ar := r.active[runID]
	r.mu.Unlock()

	if ar != nil {
		ar.cancel()
		select {
		case <-ar.done:
		case <-ctx.Done():
			return RunDetail{}, ctx.Err()
		}
		return r.GetRun(ctx, runID)
	}

	detail, err := r.GetRun(ctx, runID)
	if err != nil {
		return RunDetail{}, err
	}
	if detail.Run.Status == RunStatusRunning {
		if err := storegen.New(r.pool).CompleteRun(ctx, storegen.CompleteRunParams{
			ID:         pgUUID(runID),
			Status:     RunStatusCancelled,
			FinishedAt: pgTime(time.Now()),
		}); err != nil {
			return RunDetail{}, err
		}
		return r.GetRun(ctx, runID)
	}
	return detail, nil
}

// Advance 使用深度优先显式栈幂等推进一次执行：同一 runID 互斥，已完成节点不重复执行。
func (r *ModeRunner) Advance(ctx context.Context, runID uuid.UUID) error {
	runMu := r.lockForRun(runID)
	runMu.Lock()
	defer runMu.Unlock()

	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.maxRunDuration)
		defer cancel()
	}

	q := storegen.New(r.pool)
	run, err := q.GetRun(ctx, pgUUID(runID))
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrRunNotFound
	}
	if err != nil {
		return err
	}
	if run.Status != RunStatusRunning {
		return nil
	}

	var def ModeDefinition
	if err := json.Unmarshal(run.DefinitionSnapshot, &def); err != nil {
		return err
	}

	existingSteps, err := q.ListRunSteps(ctx, pgUUID(runID))
	if err != nil {
		return err
	}
	doneNodes := make(map[string]bool, len(existingSteps))
	for _, st := range existingSteps {
		if st.Status == StepStatusOK || st.Status == StepStatusFailed || st.Status == StepStatusSkipped {
			doneNodes[st.NodeID] = true
		}
	}

	var triggeredBy uuid.UUID
	if run.TriggeredBy.Valid {
		triggeredBy = uuid.UUID(run.TriggeredBy.Bytes)
	}
	defaultContinue := def.Defaults != nil && def.Defaults.ContinueOnError

	stack := make([]stackFrame, 0, len(def.Steps))
	stack = pushChildren(stack, def.Steps, "")

	for len(stack) > 0 {
		frame := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		node := frame.node

		if doneNodes[node.NodeID] {
			continue
		}

		if ctx.Err() != nil {
			return r.terminateOnContextErr(ctx.Err(), runID, frame, stack, doneNodes, nil)
		}

		if node.Enabled != nil && !*node.Enabled {
			now := time.Now()
			runStatus := RunStatusRunning
			if len(stack) == 0 {
				runStatus = RunStatusOK
				if err := q.CompleteRun(context.Background(), storegen.CompleteRunParams{
					ID: pgUUID(runID), Status: runStatus, FinishedAt: pgTime(now),
				}); err != nil {
					return err
				}
			}
			if err := r.saveAndBroadcast(context.Background(), RunStepMessage{
				RunID: runID, NodeID: node.NodeID, Path: frame.path, Alias: node.Alias,
				Status: StepStatusSkipped, Error: "步骤已禁用", RunStatus: runStatus,
				StartedAt: &now, FinishedAt: &now,
			}); err != nil {
				return err
			}
			doneNodes[node.NodeID] = true
			continue
		}

		startedAt := time.Now()
		if err := r.saveAndBroadcast(context.Background(), RunStepMessage{
			RunID: runID, NodeID: node.NodeID, Path: frame.path, Alias: node.Alias,
			Status: StepStatusRunning, RunStatus: RunStatusRunning, StartedAt: &startedAt,
		}); err != nil {
			return err
		}

		var stepStatus string
		var stepErr string
		var stepNote string
		abort := false

		switch node.Type {
		case "delay":
			if node.Ms > 0 {
				timer := time.NewTimer(time.Duration(node.Ms) * time.Millisecond)
				select {
				case <-timer.C:
					stepStatus = StepStatusOK
				case <-ctx.Done():
					timer.Stop()
					return r.terminateOnContextErr(ctx.Err(), runID, frame, stack, doneNodes, &startedAt)
				}
			} else {
				stepStatus = StepStatusOK
			}
		case "action":
			if node.ValueFrom == "comfort" {
				if node.ItemID == nil {
					stepStatus = StepStatusFailed
					stepErr = "舒适温控动作缺少目标空调设备"
					abort = !shouldContinue(node, defaultContinue)
				} else {
					stepStatus, stepErr, stepNote, abort = r.executeComfortAction(ctx, runID, node, triggeredBy, defaultContinue)
					if ctx.Err() != nil {
						return r.terminateOnContextErr(ctx.Err(), runID, frame, stack, doneNodes, &startedAt)
					}
				}
			} else if node.ItemID == nil || node.Capability == "" {
				stepStatus = StepStatusFailed
				stepErr = "动作节点缺少设备或能力配置"
				abort = !shouldContinue(node, defaultContinue)
			} else {
				reqID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(runID.String()+":"+node.NodeID))
				_, cmdErr := r.commands.Handle(ctx, Command{
					RequestID:     reqID,
					ItemID:        *node.ItemID,
					CapabilityKey: node.Capability,
					Value:         node.Value,
					Source:        "automation",
					CreatedBy:     triggeredBy,
				})
				if cmdErr != nil {
					if ctx.Err() != nil {
						return r.terminateOnContextErr(ctx.Err(), runID, frame, stack, doneNodes, &startedAt)
					}
					var ce *CommandError
					if errors.As(cmdErr, &ce) && ce.Code == "NOT_FOUND" {
						// SRS 异常边界：引用的设备已被删除，执行时跳过并提示。
						stepStatus = StepStatusSkipped
						stepErr = "引用的设备已被删除，已跳过"
					} else {
						stepStatus = StepStatusFailed
						stepErr = cmdErr.Error()
						abort = !shouldContinue(node, defaultContinue)
					}
				} else {
					stepStatus = StepStatusOK
				}
			}
		case "choose":
			ok, reason := r.evaluateGuard(ctx, node.When)
			if ctx.Err() != nil {
				return r.terminateOnContextErr(ctx.Err(), runID, frame, stack, doneNodes, &startedAt)
			}
			var branch []ModeNode
			if ok {
				branch = node.Then
			} else {
				branch = node.Else
			}
			if len(branch) > 0 {
				// 预先为命中分支写入 pending 步骤并逆序压栈，确保前端时间线立即可见
				for j, child := range branch {
					childPath := frame.path + "." + fmt.Sprintf("%03d", j)
					if _, err := q.UpsertRunStep(context.Background(), storegen.UpsertRunStepParams{
						RunID:  pgUUID(runID),
						NodeID: child.NodeID,
						Path:   childPath,
						Alias:  pgText(child.Alias),
						Status: StepStatusPending,
					}); err != nil {
						return err
					}
				}
				stack = pushChildren(stack, branch, frame.path)
				stepStatus = StepStatusOK
			} else {
				stepStatus = StepStatusSkipped
				if !ok && reason != "" {
					stepErr = reason
				}
			}
		default:
			stepStatus = StepStatusSkipped
			stepErr = "不支持的节点类型: " + node.Type
		}

		finishedAt := time.Now()
		runStatus := RunStatusRunning
		if abort {
			runStatus = RunStatusFailed
		} else if len(stack) == 0 {
			runStatus = RunStatusOK
		}
		if err := r.saveAndBroadcast(context.Background(), RunStepMessage{
			RunID: runID, NodeID: node.NodeID, Path: frame.path, Alias: node.Alias,
			Status: stepStatus, Error: stepErr, Note: stepNote, RunStatus: runStatus,
			StartedAt: &startedAt, FinishedAt: &finishedAt,
		}); err != nil {
			return err
		}
		doneNodes[node.NodeID] = true

		if abort {
			if err := r.skipStack(runID, stack, doneNodes, "前置步骤失败，已中止", RunStatusFailed); err != nil {
				return err
			}
			return q.CompleteRun(context.Background(), storegen.CompleteRunParams{
				ID: pgUUID(runID), Status: RunStatusFailed, FinishedAt: pgTime(finishedAt),
			})
		}
		if runStatus == RunStatusOK {
			if err := q.CompleteRun(context.Background(), storegen.CompleteRunParams{
				ID: pgUUID(runID), Status: RunStatusOK, FinishedAt: pgTime(finishedAt),
			}); err != nil {
				return err
			}
		}
	}

	return q.CompleteRun(context.Background(), storegen.CompleteRunParams{
		ID: pgUUID(runID), Status: RunStatusOK, FinishedAt: pgTime(time.Now()),
	})
}

func pushChildren(stack []stackFrame, nodes []ModeNode, prefix string) []stackFrame {
	for i := len(nodes) - 1; i >= 0; i-- {
		p := fmt.Sprintf("%03d", i)
		if prefix != "" {
			p = prefix + "." + p
		}
		stack = append(stack, stackFrame{node: nodes[i], path: p})
	}
	return stack
}

func shouldContinue(node ModeNode, defaultContinue bool) bool {
	if node.ContinueOnError != nil {
		return *node.ContinueOnError
	}
	return defaultContinue
}

// terminateOnContextErr 区分超时（DeadlineExceeded -> failed/timeout + 剩余 skipped）与主动取消（cancelled + 全部 skipped）。
func (r *ModeRunner) terminateOnContextErr(
	ctxErr error,
	runID uuid.UUID,
	current stackFrame,
	remaining []stackFrame,
	doneNodes map[string]bool,
	startedAt *time.Time,
) error {
	now := time.Now()
	if errors.Is(ctxErr, context.DeadlineExceeded) {
		stStart := startedAt
		if stStart == nil {
			stStart = &now
		}
		if !doneNodes[current.node.NodeID] {
			if err := r.saveAndBroadcast(context.Background(), RunStepMessage{
				RunID: runID, NodeID: current.node.NodeID, Path: current.path, Alias: current.node.Alias,
				Status: StepStatusFailed, Error: "timeout：执行超过最大时限", RunStatus: RunStatusFailed,
				StartedAt: stStart, FinishedAt: &now,
			}); err != nil {
				return err
			}
			doneNodes[current.node.NodeID] = true
		}
		if err := r.skipStack(runID, remaining, doneNodes, "执行超时，已终止", RunStatusFailed); err != nil {
			return err
		}
		return storegen.New(r.pool).CompleteRun(context.Background(), storegen.CompleteRunParams{
			ID: pgUUID(runID), Status: RunStatusFailed, FinishedAt: pgTime(now),
		})
	}

	all := append(remaining, current)
	if err := r.skipStack(runID, all, doneNodes, "已取消", RunStatusCancelled); err != nil {
		return err
	}
	return storegen.New(r.pool).CompleteRun(context.Background(), storegen.CompleteRunParams{
		ID: pgUUID(runID), Status: RunStatusCancelled, FinishedAt: pgTime(now),
	})
}

// skipStack 按执行顺序（栈顶到栈底）把剩余未执行节点全部标为 skipped 并广播。
func (r *ModeRunner) skipStack(runID uuid.UUID, stack []stackFrame, doneNodes map[string]bool, reason string, runStatus string) error {
	now := time.Now()
	for i := len(stack) - 1; i >= 0; i-- {
		frame := stack[i]
		if doneNodes[frame.node.NodeID] {
			continue
		}
		if err := r.saveAndBroadcast(context.Background(), RunStepMessage{
			RunID: runID, NodeID: frame.node.NodeID, Path: frame.path, Alias: frame.node.Alias,
			Status: StepStatusSkipped, Error: reason, RunStatus: runStatus,
			StartedAt: &now, FinishedAt: &now,
		}); err != nil {
			return err
		}
		doneNodes[frame.node.NodeID] = true
	}
	return nil
}

// evaluateGuard 统一求值顶层 guard 与 choose.when；传感器读数缺失按不成立处理并给出具体原因。
func (r *ModeRunner) evaluateGuard(ctx context.Context, guard *ModeGuard) (bool, string) {
	if guard == nil {
		return true, ""
	}
	if len(guard.All) > 0 {
		for _, cond := range guard.All {
			ok, reason := r.evaluateCondition(ctx, cond)
			if !ok {
				return false, reason
			}
		}
	}
	if len(guard.Any) > 0 {
		var reasons []string
		matched := false
		for _, cond := range guard.Any {
			ok, reason := r.evaluateCondition(ctx, cond)
			if ok {
				matched = true
				break
			}
			reasons = append(reasons, reason)
		}
		if !matched {
			return false, "任一守卫条件均未满足：" + strings.Join(reasons, "；")
		}
	}
	return true, ""
}

func (r *ModeRunner) evaluateCondition(ctx context.Context, cond ModeCondition) (bool, string) {
	q := storegen.New(r.pool)
	label := cond.ItemID.String()
	if item, err := q.GetItem(ctx, pgUUID(cond.ItemID)); err == nil && item.Name != "" {
		label = item.Name
	}
	state, err := q.GetState(ctx, storegen.GetStateParams{
		ItemID:        pgUUID(cond.ItemID),
		CapabilityKey: cond.Capability,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Sprintf("守卫条件不满足：「%s」的 %s 读数缺失", label, cond.Capability)
	}
	if err != nil {
		return false, fmt.Sprintf("守卫条件不满足：读取「%s」的 %s 失败", label, cond.Capability)
	}

	var actual any
	if err := json.Unmarshal(state.Value, &actual); err != nil || actual == nil || actual == "unavailable" {
		return false, fmt.Sprintf("守卫条件不满足：「%s」的 %s 读数缺失或不可用", label, cond.Capability)
	}

	ok, err := compareValues(actual, cond.Op, cond.Value)
	if err != nil {
		return false, fmt.Sprintf("守卫条件不满足：「%s」的 %s 无法比较（%v）", label, cond.Capability, err)
	}
	if !ok {
		return false, fmt.Sprintf("守卫条件不满足：「%s」的 %s 当前为 %v（要求 %s %v）", label, cond.Capability, actual, cond.Op, cond.Value)
	}
	return true, ""
}

func compareValues(actual any, op string, expected any) (bool, error) {
	aNum, aIsNum := toFloat64(actual)
	eNum, eIsNum := toFloat64(expected)
	if aIsNum && eIsNum {
		switch op {
		case "==":
			return aNum == eNum, nil
		case "!=":
			return aNum != eNum, nil
		case ">":
			return aNum > eNum, nil
		case ">=":
			return aNum >= eNum, nil
		case "<":
			return aNum < eNum, nil
		case "<=":
			return aNum <= eNum, nil
		default:
			return false, fmt.Errorf("不支持的运算符 %s", op)
		}
	}
	switch op {
	case "==":
		return fmt.Sprintf("%v", actual) == fmt.Sprintf("%v", expected), nil
	case "!=":
		return fmt.Sprintf("%v", actual) != fmt.Sprintf("%v", expected), nil
	default:
		return false, fmt.Errorf("非数值类型不支持运算符 %s", op)
	}
}

func toFloat64(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int32:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

// executeComfortAction 收集房间温湿度与空调当前状态，调用纯策略层 EvaluateComfort，并按决策下发指令。
func (r *ModeRunner) executeComfortAction(
	ctx context.Context,
	runID uuid.UUID,
	node ModeNode,
	triggeredBy uuid.UUID,
	defaultContinue bool,
) (status, errMsg, note string, abort bool) {
	q := storegen.New(r.pool)
	acItem, err := q.GetItem(ctx, pgUUID(*node.ItemID))
	if errors.Is(err, pgx.ErrNoRows) {
		return StepStatusSkipped, "引用的设备已被删除，已跳过", "", false
	}
	if err != nil {
		return StepStatusFailed, err.Error(), "", !shouldContinue(node, defaultContinue)
	}

	indoorTemp, indoorRH := r.readRoomTempHumidity(ctx, acItem.RoomHash)
	acStates, _ := q.ListStatesByItem(ctx, pgUUID(*node.ItemID))
	var currentOn bool
	var currentMode string
	var currentTarget *float64
	var lastSwitchAt *time.Time
	for _, st := range acStates {
		var v any
		if err := json.Unmarshal(st.Value, &v); err != nil {
			continue
		}
		switch st.CapabilityKey {
		case "on":
			if b, ok := v.(bool); ok {
				currentOn = b
			}
			if st.UpdatedAt.Valid {
				t := st.UpdatedAt.Time
				lastSwitchAt = &t
			}
		case "mode":
			if s, ok := v.(string); ok {
				currentMode = s
			}
		case "target_temp":
			if f, ok := toFloat64(v); ok {
				currentTarget = &f
			}
		}
	}

	now := time.Now()
	env := r.simEnv.Get(now)
	// 若用户刚刚通过 /api/sim/env 切换了季节或室外环境，不受旧季节的 60 秒最小切换间隔阻塞。
	effectiveLastSwitch := lastSwitchAt
	if lastSwitchAt != nil && !env.UpdatedAt.IsZero() && env.UpdatedAt.After(*lastSwitchAt) {
		effectiveLastSwitch = nil
	}
	dec := policy.EvaluateComfort(policy.ComfortInput{
		Season:          env.EffectiveSeason,
		IndoorTemp:      indoorTemp,
		IndoorHumidity:  indoorRH,
		OutdoorTemp:     env.OutdoorTemp,
		OutdoorHumidity: env.OutdoorHumidity,
		CurrentOn:       currentOn,
		CurrentMode:     currentMode,
		CurrentTarget:   currentTarget,
		LastSwitchAt:    effectiveLastSwitch,
		Now:             now,
	})

	switch dec.Action {
	case policy.ComfortActionWait:
		return StepStatusSkipped, dec.Reason, dec.Reason, false
	case policy.ComfortActionHold:
		return StepStatusOK, "", dec.Reason, false
	case policy.ComfortActionNatural:
		if currentOn {
			reqID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(runID.String()+":"+node.NodeID+":on"))
			if _, cmdErr := r.commands.Handle(ctx, Command{
				RequestID: reqID, ItemID: *node.ItemID, CapabilityKey: "on", Value: false, Source: "automation", CreatedBy: triggeredBy,
			}); cmdErr != nil {
				return StepStatusFailed, cmdErr.Error(), dec.Reason, !shouldContinue(node, defaultContinue)
			}
		}
		return StepStatusOK, "", dec.Reason, false
	case policy.ComfortActionCool, policy.ComfortActionHeat:
		cmds := []struct {
			suffix string
			key    string
			val    any
		}{
			{"mode", "mode", dec.ACMode},
			{"target_temp", "target_temp", *dec.TargetTemp},
		}
		// 仅在状态确实需要从关变为开时下发 on=true，避免无意义刷新 on.updated_at 重置 60 秒计时器
		if !currentOn {
			cmds = append(cmds, struct {
				suffix string
				key    string
				val    any
			}{"on", "on", true})
		}
		for _, c := range cmds {
			reqID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(runID.String()+":"+node.NodeID+":"+c.suffix))
			if _, cmdErr := r.commands.Handle(ctx, Command{
				RequestID: reqID, ItemID: *node.ItemID, CapabilityKey: c.key, Value: c.val, Source: "automation", CreatedBy: triggeredBy,
			}); cmdErr != nil {
				return StepStatusFailed, cmdErr.Error(), dec.Reason, !shouldContinue(node, defaultContinue)
			}
		}
		return StepStatusOK, "", dec.Reason, false
	default:
		return StepStatusOK, "", dec.Reason, false
	}
}

func collectModeYieldKeys(nodes []ModeNode) []YieldKey {
	var out []YieldKey
	for _, n := range nodes {
		if n.Enabled != nil && !*n.Enabled {
			continue
		}
		if n.Type == "action" && n.ItemID != nil {
			if n.ValueFrom == "comfort" {
				out = append(out,
					YieldKey{ItemID: *n.ItemID, CapabilityKey: "on"},
					YieldKey{ItemID: *n.ItemID, CapabilityKey: "mode"},
					YieldKey{ItemID: *n.ItemID, CapabilityKey: "target_temp"},
				)
			} else if n.Capability != "" {
				out = append(out, YieldKey{ItemID: *n.ItemID, CapabilityKey: n.Capability})
			}
		}
		if len(n.Then) > 0 {
			out = append(out, collectModeYieldKeys(n.Then)...)
		}
		if len(n.Else) > 0 {
			out = append(out, collectModeYieldKeys(n.Else)...)
		}
	}
	return out
}

func (r *ModeRunner) readRoomTempHumidity(ctx context.Context, roomHash pgtype.Text) (*float64, *float64) {
	readings, _ := readRoomEnvReadings(ctx, r.pool, roomHash, true)
	return readings.Temperature, readings.Humidity
}

// saveAndBroadcast 严格遵循 ADR 0002 单一写入点：先落库成功，再由 Broadcaster 广播。
func (r *ModeRunner) saveAndBroadcast(ctx context.Context, msg RunStepMessage) error {
	var startPG, finishPG pgtype.Timestamptz
	if msg.StartedAt != nil {
		startPG = pgTime(*msg.StartedAt)
	}
	if msg.FinishedAt != nil {
		finishPG = pgTime(*msg.FinishedAt)
	}
	if _, err := storegen.New(r.pool).UpsertRunStep(ctx, storegen.UpsertRunStepParams{
		RunID:      pgUUID(msg.RunID),
		NodeID:     msg.NodeID,
		Path:       msg.Path,
		Alias:      pgText(msg.Alias),
		Status:     msg.Status,
		Error:      pgText(msg.Error),
		Note:       pgText(msg.Note),
		StartedAt:  startPG,
		FinishedAt: finishPG,
	}); err != nil {
		return err
	}
	if r.broadcaster != nil {
		r.broadcaster.BroadcastRunStep(msg)
	}
	return nil
}
