package api

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api/gen"
	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/control"
)

// 坐标单位是厘米，原点在平面图左上角。
// 上限 50 m：够画一栋别墅，也挡住"少写个小数点"这种把画布撑爆的输入。
const (
	minWallLength = 1.0
	maxCoordinate = 5000.0
	maxWallThick  = 100.0
	defaultThick  = 10.0
)

// PlanCorner 是户型里的一个端点。
type PlanCorner struct {
	ID openapi_types.UUID
	X  float64
	Y  float64
}

// PlanWall 是一面墙，带上两端坐标（读取时 join 好，避免前端自己做两次查找）。
type PlanWall struct {
	ID        openapi_types.UUID
	AID       openapi_types.UUID
	BID       openapi_types.UUID
	AX, AY    float64
	BX, BY    float64
	Thickness float64
}

// WallInput 是画墙的输入。
type WallInput struct {
	AX, AY    float64
	BX, BY    float64
	Thickness float64
}

// 改墙时的两种失败：墙不存在、改完与别的墙重合。
var (
	ErrWallNotFound = errors.New("墙不存在")
	ErrWallConflict = errors.New("改完与另一面墙重合")
	// ErrPlanConflict 版本号过期：别处已经改过这份户型。
	ErrPlanConflict = errors.New("plan version conflict")
)

// RoomName 是用户给某个房间起的名字，键是房间多边形的哈希。
type RoomName struct {
	Hash string
	Name string
	Kind string
}

// PlanStore 保存户型。
//
// 写操作：画墙、改墙、删墙。端点去重与「同一对端点只留一面墙」属于持久化语义，
// 放在实现里（PGPlan），接口层不掺和。
// WallSnapshot 是一面墙的完整几何；ID 为空表示新画的一面墙。
type WallSnapshot struct {
	ID        *string
	AX, AY    float64
	BX, BY    float64
	Thickness float64
}

type PlanStore interface {
	LoadPlan(ctx context.Context) ([]PlanCorner, []PlanWall, error)
	// LoadRoomNames 只取"名字"，几何由墙推导——房间不存几何是 ADR 0002 定下的。
	LoadRoomNames(ctx context.Context) ([]RoomName, error)
	CreateWall(ctx context.Context, in WallInput) error
	UpdateWall(ctx context.Context, id openapi_types.UUID, in WallInput) error
	DeleteWall(ctx context.Context, id openapi_types.UUID) (int64, error)
	// LoadVersion 读平面图版本号；撤销/重做拿它做乐观并发。
	LoadVersion(ctx context.Context) (int64, error)
	// ReplacePlan 按 id 对齐地整份替换墙列表，成功后版本号 +1。
	ReplacePlan(ctx context.Context, version int64, walls []WallSnapshot) error
}

// GetPlan 返回整张户型。端点单独列，因为两面墙共享一个端点。
func (s *Server) GetPlan(w http.ResponseWriter, r *http.Request) {
	plan, err := s.readPlan(r.Context())
	if err != nil {
		s.log.Error("读取户型失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取户型失败")
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

// CreateWall 画一面墙：端点按坐标去重，同一对端点只留一面墙。
// 成功后返回整个户型，前端直接替换本地状态。
func (s *Server) CreateWall(w http.ResponseWriter, r *http.Request) {
	var body gen.WallInput

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "请求体不是合法的 JSON")
		return
	}

	thickness := defaultThick
	if body.Thickness != nil {
		thickness = float64(*body.Thickness)
	}
	in := WallInput{
		AX: float64(body.Ax), AY: float64(body.Ay),
		BX: float64(body.Bx), BY: float64(body.By),
		Thickness: thickness,
	}
	if msg, ok := validateWall(in); !ok {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, msg)
		return
	}

	ctx := r.Context()
	if err := s.plan.CreateWall(ctx, in); err != nil {
		s.log.Error("保存墙失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "保存失败")
		return
	}

	plan, err := s.readPlan(ctx)
	if err != nil {
		s.log.Error("读取户型失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取户型失败")
		return
	}
	writeJSON(w, http.StatusCreated, plan)
}

// DeleteWall 删墙。端点保留：它可能被别的墙共用，孤立端点本身是合法状态。
// UpdateWall 改一面墙的端点或厚度——拖端点微调长度与角度走这条。
// 端点仍按坐标去重，所以把端点拖到别处已有端点上等于合并到这个点。
func (s *Server) UpdateWall(w http.ResponseWriter, r *http.Request, wallID openapi_types.UUID) {
	var body gen.WallInput

	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "请求体不是合法的 JSON")
		return
	}

	thickness := defaultThick
	if body.Thickness != nil {
		thickness = float64(*body.Thickness)
	}
	in := WallInput{
		AX: float64(body.Ax), AY: float64(body.Ay),
		BX: float64(body.Bx), BY: float64(body.By),
		Thickness: thickness,
	}
	if msg, ok := validateWall(in); !ok {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, msg)
		return
	}

	ctx := r.Context()
	switch err := s.plan.UpdateWall(ctx, wallID, in); {
	case err == nil:
	case errors.Is(err, ErrWallNotFound):
		writeError(w, http.StatusNotFound, gen.NOTFOUND, "没有这面墙")
		return
	case errors.Is(err, ErrWallConflict):
		writeError(w, http.StatusConflict, gen.CONFLICT, "这个位置已经有一面同样的墙了")
		return
	default:
		s.log.Error("改墙失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "保存失败")
		return
	}

	plan, err := s.readPlan(ctx)
	if err != nil {
		s.log.Error("读取户型失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取户型失败")
		return
	}
	writeJSON(w, http.StatusOK, plan)
}

func (s *Server) DeleteWall(w http.ResponseWriter, r *http.Request, wallID openapi_types.UUID) {
	n, err := s.plan.DeleteWall(r.Context(), wallID)
	if err != nil {
		s.log.Error("删除墙失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "删除失败")
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, gen.NOTFOUND, "没有这面墙")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// readPlan 读一次全量户型并投影成契约里的形状。
func (s *Server) readPlan(ctx context.Context) (gen.Plan, error) {
	corners, walls, err := s.plan.LoadPlan(ctx)
	if err != nil {
		return gen.Plan{}, err
	}

	version, err := s.plan.LoadVersion(ctx)
	if err != nil {
		return gen.Plan{}, err
	}

	plan := gen.Plan{
		Corners: make([]gen.Corner, 0, len(corners)),
		Walls:   make([]gen.Wall, 0, len(walls)),
		Version: version,
	}
	for _, c := range corners {
		plan.Corners = append(plan.Corners, gen.Corner{Id: c.ID, X: c.X, Y: c.Y})
	}
	// 房间由后端唯一推导：前端拿到的是结果，不自己算——两套实现迟早会分叉。
	points := make([]control.Point, 0, len(corners))
	for _, c := range corners {
		points = append(points, control.Point{ID: c.ID.String(), X: c.X, Y: c.Y})
	}
	edges := make([]control.Wall, 0, len(walls))
	for _, w := range walls {
		edges = append(edges, control.Wall{A: w.AID.String(), B: w.BID.String()})
	}
	derived := control.Derive(points, edges)

	names, err := s.plan.LoadRoomNames(ctx)
	if err != nil {
		return gen.Plan{}, err
	}
	named := make(map[string]RoomName, len(names))
	for _, n := range names {
		named[n.Hash] = n
	}

	plan.Rooms = make([]gen.Room, 0, len(derived.Rings))
	for _, ring := range derived.Rings {
		pts := make([]gen.PlanPoint, 0, len(ring.Points))
		for _, p := range ring.Points {
			pts = append(pts, gen.PlanPoint{X: p.X, Y: p.Y})
		}
		room := gen.Room{Hash: ring.Hash, Ring: pts, AreaM2: ring.AreaM2}
		if n, ok := named[ring.Hash]; ok {
			name := n.Name
			room.Name = &name
			kind := gen.RoomKind(n.Kind)
			room.Kind = &kind
		}
		plan.Rooms = append(plan.Rooms, room)
	}
	plan.OpenWalls = derived.OpenWalls

	for _, w := range walls {
		plan.Walls = append(plan.Walls, gen.Wall{
			Id:        w.ID,
			AId:       w.AID,
			BId:       w.BID,
			Ax:        w.AX,
			Ay:        w.AY,
			Bx:        w.BX,
			By:        w.BY,
			Thickness: w.Thickness,
		})
	}
	return plan, nil
}

// validateWall 只挡明显错误的输入：坐标越界、两端重合、墙厚离谱。
// 不做"墙必须正交"之类的判断——斜墙是合法的，房间推导会处理。
func validateWall(in WallInput) (string, bool) {
	for _, v := range []float64{in.AX, in.AY, in.BX, in.BY} {
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return "坐标必须是有限数值", false
		}
		if v < 0 || v > maxCoordinate {
			return "坐标超出画布范围（0–5000 厘米）", false
		}
	}
	if math.Hypot(in.BX-in.AX, in.BY-in.AY) < minWallLength {
		return "墙的两个端点重合了", false
	}
	if in.Thickness < 1 || in.Thickness > maxWallThick {
		return "墙厚必须在 1–100 厘米之间", false
	}
	return "", true
}

// ReplacePlan 整份替换户型，供撤销/重做使用。
//
// 不重放操作是因为墙的 id 由服务端生成：删了再建会换 id，而门窗的
// `wall_id`（T5 起）与前端选中态都挂在它上面。整份替换按 id 对齐，
// 保住 id 才谈得上「撤销一次删除」。
func (s *Server) ReplacePlan(w http.ResponseWriter, r *http.Request) {
	var in gen.PlanInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 256<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "请求体不合法")
		return
	}

	walls := make([]WallSnapshot, 0, len(in.Walls))
	for _, wall := range in.Walls {
		thickness := 10.0
		if wall.Thickness != nil {
			thickness = *wall.Thickness
		}
		if msg, ok := validateWall(WallInput{
			AX: wall.Ax, AY: wall.Ay, BX: wall.Bx, BY: wall.By, Thickness: thickness,
		}); !ok {
			writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, msg)
			return
		}
		var id *string
		if wall.Id != nil {
			s := wall.Id.String()
			id = &s
		}
		walls = append(walls, WallSnapshot{
			ID: id, AX: wall.Ax, AY: wall.Ay, BX: wall.Bx, BY: wall.By, Thickness: thickness,
		})
	}

	if err := s.plan.ReplacePlan(r.Context(), in.Version, walls); err != nil {
		if errors.Is(err, ErrPlanConflict) {
			writeError(w, http.StatusConflict, gen.CONFLICT, "户型已被他人修改，请刷新后重试")
			return
		}
		s.log.Error("替换户型失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "服务器出了点问题")
		return
	}

	plan, err := s.readPlan(r.Context())
	if err != nil {
		s.log.Error("替换后读取户型失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "服务器出了点问题")
		return
	}
	writeJSON(w, http.StatusOK, plan)
}
