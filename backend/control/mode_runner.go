package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

var (
	ErrModeNotFound = errors.New("场景不存在")
	ErrRunNotFound  = errors.New("执行记录不存在")
)

const (
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
	Run   storegen.Run
	Steps []storegen.RunStep
}

type activeRun struct {
	cancel context.CancelFunc
	done   chan struct{}
}

// ModeRunner 负责场景执行生命周期：建 run、幂等推进步骤、取消与广播进度。
type ModeRunner struct {
	pool        *pgxpool.Pool
	commands    CommandHandler
	broadcaster RunStepBroadcaster

	mu          sync.Mutex
	active      map[uuid.UUID]*activeRun
	advanceLock map[uuid.UUID]*sync.Mutex
}

func NewModeRunner(pool *pgxpool.Pool, commands CommandHandler, broadcaster RunStepBroadcaster) *ModeRunner {
	return &ModeRunner{
		pool:        pool,
		commands:    commands,
		broadcaster: broadcaster,
		active:      make(map[uuid.UUID]*activeRun),
		advanceLock: make(map[uuid.UUID]*sync.Mutex),
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
	return RunDetail{Run: run, Steps: steps}, nil
}

// Start 创建一条 run 及其初始 pending 步骤，并在后台异步推进。
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

	var def ModeDefinition
	if err := json.Unmarshal(mode.Definition, &def); err != nil {
		return RunDetail{}, fmt.Errorf("解析场景定义失败: %w", err)
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

	runCtx, cancel := context.WithCancel(context.Background())
	ar := &activeRun{cancel: cancel, done: make(chan struct{})}
	r.mu.Lock()
	r.active[runID] = ar
	r.mu.Unlock()

	go func() {
		defer func() {
			cancel()
			r.mu.Lock()
			delete(r.active, runID)
			delete(r.advanceLock, runID)
			r.mu.Unlock()
			close(ar.done)
		}()
		_ = r.Advance(runCtx, runID)
	}()

	return RunDetail{Run: run, Steps: steps}, nil
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

// Advance 幂等推进一次执行：同一 runID 互斥执行，已处于终态（ok/failed/skipped）的节点不会重复执行。
func (r *ModeRunner) Advance(ctx context.Context, runID uuid.UUID) error {
	runMu := r.lockForRun(runID)
	runMu.Lock()
	defer runMu.Unlock()

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

	for i, node := range def.Steps {
		if doneNodes[node.NodeID] {
			continue
		}
		path := fmt.Sprintf("%03d", i)
		isLast := i == len(def.Steps)-1

		if ctx.Err() != nil {
			return r.cancelRemaining(runID, def.Steps[i:], i, doneNodes)
		}

		if node.Enabled != nil && !*node.Enabled {
			now := time.Now()
			runStatus := RunStatusRunning
			if isLast {
				runStatus = RunStatusOK
				if err := q.CompleteRun(context.Background(), storegen.CompleteRunParams{
					ID: pgUUID(runID), Status: runStatus, FinishedAt: pgTime(now),
				}); err != nil {
					return err
				}
			}
			if err := r.saveAndBroadcast(context.Background(), RunStepMessage{
				RunID: runID, NodeID: node.NodeID, Path: path, Alias: node.Alias,
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
			RunID: runID, NodeID: node.NodeID, Path: path, Alias: node.Alias,
			Status: StepStatusRunning, RunStatus: RunStatusRunning, StartedAt: &startedAt,
		}); err != nil {
			return err
		}

		var stepStatus string
		var stepErr string
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
					return r.cancelRemaining(runID, def.Steps[i:], i, doneNodes)
				}
			} else {
				stepStatus = StepStatusOK
			}
		case "action":
			if node.ValueFrom == "comfort" {
				stepStatus = StepStatusSkipped
				stepErr = "动态舒适温控待 T13 接入"
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
					CreatedBy:     triggeredBy,
				})
				if cmdErr != nil {
					if ctx.Err() != nil {
						return r.cancelRemaining(runID, def.Steps[i:], i, doneNodes)
					}
					stepStatus = StepStatusFailed
					stepErr = cmdErr.Error()
					abort = !shouldContinue(node, defaultContinue)
				} else {
					stepStatus = StepStatusOK
				}
			}
		case "choose":
			stepStatus = StepStatusSkipped
			stepErr = "条件分支待 T12 接入"
		default:
			stepStatus = StepStatusSkipped
			stepErr = "不支持的节点类型: " + node.Type
		}

		finishedAt := time.Now()
		runStatus := RunStatusRunning
		if abort {
			runStatus = RunStatusFailed
		} else if isLast {
			runStatus = RunStatusOK
		}
		if runStatus == RunStatusOK || runStatus == RunStatusFailed {
			if err := q.CompleteRun(context.Background(), storegen.CompleteRunParams{
				ID: pgUUID(runID), Status: runStatus, FinishedAt: pgTime(finishedAt),
			}); err != nil {
				return err
			}
		}
		if err := r.saveAndBroadcast(context.Background(), RunStepMessage{
			RunID: runID, NodeID: node.NodeID, Path: path, Alias: node.Alias,
			Status: stepStatus, Error: stepErr, RunStatus: runStatus,
			StartedAt: &startedAt, FinishedAt: &finishedAt,
		}); err != nil {
			return err
		}
		doneNodes[node.NodeID] = true

		if abort {
			return r.skipRemaining(runID, def.Steps[i+1:], i+1, doneNodes, "前置步骤失败，已中止", RunStatusFailed)
		}
	}

	return q.CompleteRun(context.Background(), storegen.CompleteRunParams{
		ID: pgUUID(runID), Status: RunStatusOK, FinishedAt: pgTime(time.Now()),
	})
}

func shouldContinue(node ModeNode, defaultContinue bool) bool {
	if node.ContinueOnError != nil {
		return *node.ContinueOnError
	}
	return defaultContinue
}

func (r *ModeRunner) cancelRemaining(runID uuid.UUID, remaining []ModeNode, startIdx int, doneNodes map[string]bool) error {
	if err := storegen.New(r.pool).CompleteRun(context.Background(), storegen.CompleteRunParams{
		ID: pgUUID(runID), Status: RunStatusCancelled, FinishedAt: pgTime(time.Now()),
	}); err != nil {
		return err
	}
	return r.skipRemaining(runID, remaining, startIdx, doneNodes, "已取消", RunStatusCancelled)
}

func (r *ModeRunner) skipRemaining(runID uuid.UUID, remaining []ModeNode, startIdx int, doneNodes map[string]bool, reason string, runStatus string) error {
	now := time.Now()
	for offset, node := range remaining {
		if doneNodes[node.NodeID] {
			continue
		}
		path := fmt.Sprintf("%03d", startIdx+offset)
		if err := r.saveAndBroadcast(context.Background(), RunStepMessage{
			RunID: runID, NodeID: node.NodeID, Path: path, Alias: node.Alias,
			Status: StepStatusSkipped, Error: reason, RunStatus: runStatus,
			StartedAt: &now, FinishedAt: &now,
		}); err != nil {
			return err
		}
		doneNodes[node.NodeID] = true
	}
	return nil
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
