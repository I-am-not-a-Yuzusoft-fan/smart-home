package api

import (
	"context"
	"encoding/json"
	"math"
	"net/http"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api/gen"
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

// PlanStore 保存户型。
//
// 只有两个写操作：画一面墙、删一面墙。端点去重与「同一对端点只留一面墙」
// 属于持久化语义，放在实现里（PGPlan），接口层不掺和。
type PlanStore interface {
	LoadPlan(ctx context.Context) ([]PlanCorner, []PlanWall, error)
	CreateWall(ctx context.Context, in WallInput) error
	DeleteWall(ctx context.Context, id openapi_types.UUID) (int64, error)
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

	plan := gen.Plan{
		Corners: make([]gen.Corner, 0, len(corners)),
		Walls:   make([]gen.Wall, 0, len(walls)),
	}
	for _, c := range corners {
		plan.Corners = append(plan.Corners, gen.Corner{Id: c.ID, X: c.X, Y: c.Y})
	}
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
