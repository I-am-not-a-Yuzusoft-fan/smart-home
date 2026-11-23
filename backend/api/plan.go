package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"strings"
	"unicode/utf8"

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
	// ArcExtent 弧度（度）：0 为直墙，正负表示往弦的哪一侧弯。
	ArcExtent float64
}

// WallInput 是画墙的输入。
type WallInput struct {
	AX, AY    float64
	BX, BY    float64
	Thickness float64
	ArcExtent float64
}

// PlanOpening 是一个门或窗。和挂墙物品一样用「墙 + 沿墙偏移」定位，
// 墙被拖动或改长后偏移不变、世界坐标跟着墙走。
type PlanOpening struct {
	ID     openapi_types.UUID
	WallID openapi_types.UUID
	Kind   string
	Offset float64
	Width  float64
	Height float64
	Sill   float64
}

// OpeningInput 是开门/开窗的输入；Height、Sill 省略时按类型给默认值。
type OpeningInput struct {
	WallID openapi_types.UUID
	Kind   string
	Offset float64
	Width  float64
	Height *float64
	Sill   *float64
}

// OpeningPatchInput 是逐字段 PATCH 的输入。
type OpeningPatchInput struct {
	OffsetSet bool
	Offset    float64
	WidthSet  bool
	Width     float64
	HeightSet bool
	Height    float64
	SillSet   bool
	Sill      float64
}

// 改墙时的两种失败：墙不存在、改完与别的墙重合。
var (
	ErrWallNotFound = errors.New("墙不存在")
	ErrWallConflict = errors.New("改完与另一面墙重合")
	// ErrOpeningNotFound 开口不存在；ErrOpeningTooWide 开口超出墙长。
	ErrOpeningNotFound = errors.New("开口不存在")
	ErrOpeningTooWide  = errors.New("开口超出墙的范围")
	// ErrArcWall 挂在弧墙上：v1 只在直墙上开门窗、挂物品。
	ErrArcWall = errors.New("弧墙暂不支持门窗与挂墙物品")
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
	ArcExtent float64
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
	// UpsertRoomName 只写"名字与用途"，不碰几何。
	UpsertRoomName(ctx context.Context, hash, name, kind string) error
	// DeleteRoomName 清掉某个哈希上的名字；本来就没有也返回 0，不算错。
	DeleteRoomName(ctx context.Context, hash string) (int64, error)
	// LoadOpenings 读全部门窗，随 Plan 一次返回，前端不必再发一次请求。
	LoadOpenings(ctx context.Context) ([]PlanOpening, error)
	// GetWall 读单面墙的端点：挂墙物品与门窗换算位置时要用。
	GetWall(ctx context.Context, id openapi_types.UUID) (PlanWall, error)
	CreateOpening(ctx context.Context, in OpeningInput) (openapi_types.UUID, error)
	UpdateOpening(ctx context.Context, id openapi_types.UUID, in OpeningPatchInput) error
	DeleteOpening(ctx context.Context, id openapi_types.UUID) (int64, error)
	// LoadUnderlay 读底图元数据；没有底图时返回 nil, nil。
	LoadUnderlay(ctx context.Context) (*PlanUnderlay, error)
	// LoadUnderlayImage 单独取字节：元数据要随 Plan 每次返回，图片不该跟着 JSON 走。
	LoadUnderlayImage(ctx context.Context) (contentType string, data []byte, sha256 string, err error)
	// SaveUnderlay 整张替换底图，并把定标与显示参数重置。
	SaveUnderlay(ctx context.Context, in UnderlayUpload) error
	// UpdateUnderlayMeta 只改出现的字段，返回改后的元数据。
	UpdateUnderlayMeta(ctx context.Context, patch UnderlayMetaPatch) (*PlanUnderlay, error)
	// DeleteUnderlay 删掉底图与字节；本来就没有也返回 0。
	DeleteUnderlay(ctx context.Context) (int64, error)
}

// placeOnWall 把「沿墙偏移」换算成世界坐标与朝向，并把偏移夹进墙内。
// 物品/开口比墙还宽时偏移取 0——宁可压在墙上，也不要跑到墙外。
func placeOnWall(w PlanWall, offset, width float64) (x, y, rotation, clamped float64) {
	dx, dy := w.BX-w.AX, w.BY-w.AY
	length := math.Hypot(dx, dy)
	if length == 0 {
		return w.AX, w.AY, 0, 0
	}
	maxOffset := math.Max(0, length-width)
	if offset < 0 {
		offset = 0
	}
	if offset > maxOffset {
		offset = maxOffset
	}
	ux, uy := dx/length, dy/length
	x = w.AX + ux*offset
	y = w.AY + uy*offset
	// atan2 在屏幕坐标系（y 向下）里正好是顺时针为正，与 rotation 的约定一致。
	rotation = math.Atan2(dy, dx) * 180 / math.Pi
	return x, y, rotation, offset
}

// roomGoneMessage 是房间哈希对不上时的统一文案：墙改过、别人改过、页面太旧，
// 三种原因对用户是同一件事——刷新重来。
const roomGoneMessage = "房间不存在或已变化，请刷新后重试"

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
	arc := 0.0
	if body.ArcExtent != nil {
		arc = float64(*body.ArcExtent)
	}
	in := WallInput{
		AX: float64(body.Ax), AY: float64(body.Ay),
		BX: float64(body.Bx), BY: float64(body.By),
		Thickness: thickness,
		ArcExtent: arc,
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

	// 拖端点只发坐标，没带 thickness/arcExtent 时要沿用原值，
	// 否则会把 20 厘米外墙改回 10、把弧墙拉直。
	existing, err := s.plan.GetWall(r.Context(), wallID)
	if err != nil {
		if errors.Is(err, ErrWallNotFound) {
			writeError(w, http.StatusNotFound, gen.NOTFOUND, "没有这面墙")
			return
		}
		s.log.Error("读墙失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取失败")
		return
	}

	thickness := existing.Thickness
	if body.Thickness != nil {
		thickness = float64(*body.Thickness)
	}
	arc := existing.ArcExtent
	if body.ArcExtent != nil {
		arc = float64(*body.ArcExtent)
	}
	in := WallInput{
		AX: float64(body.Ax), AY: float64(body.Ay),
		BX: float64(body.Bx), BY: float64(body.By),
		Thickness: thickness,
		ArcExtent: arc,
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
// CreateOpening 在墙上开一个门或窗。
func (s *Server) CreateOpening(w http.ResponseWriter, r *http.Request) {
	var body gen.OpeningInput
	if !decodeJSONBody(w, r, &body) {
		return
	}
	kind := string(body.Kind)
	if kind != "door" && kind != "window" {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "kind 只能是 door 或 window")
		return
	}
	if body.Width < 10 || body.Width > 500 {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "开口宽度必须在 10–500 厘米之间")
		return
	}
	if body.Height != nil && (*body.Height < 10 || *body.Height > 500) {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "开口高度必须在 10–500 厘米之间")
		return
	}
	_, err := s.plan.CreateOpening(r.Context(), OpeningInput{
		WallID: body.WallId,
		Kind:   kind,
		Offset: body.Offset,
		Width:  body.Width,
		Height: body.Height,
		Sill:   body.Sill,
	})
	switch {
	case err == nil:
	case errors.Is(err, ErrWallNotFound):
		writeError(w, http.StatusNotFound, gen.NOTFOUND, "没有这面墙")
		return
	case errors.Is(err, ErrOpeningTooWide):
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "开口超出了这面墙的范围")
		return
	case errors.Is(err, ErrArcWall):
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "弧墙暂不支持门窗：先把它改成直墙再开洞")
		return
	default:
		s.log.Error("开洞失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "保存失败")
		return
	}
	w.WriteHeader(http.StatusCreated)
	plan, err := s.readPlan(r.Context())
	if err != nil {
		s.log.Error("读取户型失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取户型失败")
		return
	}
	writeJSON(w, http.StatusCreated, plan)
}

// UpdateOpening 调整开口的位置或尺寸。
func (s *Server) UpdateOpening(w http.ResponseWriter, r *http.Request, openingID openapi_types.UUID) {
	var body gen.OpeningPatch
	if !decodeJSONBody(w, r, &body) {
		return
	}
	in := OpeningPatchInput{}
	if body.Offset != nil {
		in.OffsetSet, in.Offset = true, float64(*body.Offset)
	}
	if body.Width != nil {
		in.WidthSet, in.Width = true, float64(*body.Width)
	}
	if body.Height != nil {
		in.HeightSet, in.Height = true, float64(*body.Height)
	}
	if body.Sill != nil {
		in.SillSet, in.Sill = true, float64(*body.Sill)
	}
	err := s.plan.UpdateOpening(r.Context(), openingID, in)
	switch {
	case err == nil:
	case errors.Is(err, ErrOpeningNotFound):
		writeError(w, http.StatusNotFound, gen.NOTFOUND, "开口不存在")
		return
	case errors.Is(err, ErrOpeningTooWide):
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "开口超出了这面墙的范围")
		return
	default:
		s.log.Error("改开口失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "保存失败")
		return
	}
	s.writePlan(w, r)
}

// DeleteOpening 删掉一个开口，幂等。
func (s *Server) DeleteOpening(w http.ResponseWriter, r *http.Request, openingID openapi_types.UUID) {
	if _, err := s.plan.DeleteOpening(r.Context(), openingID); err != nil {
		s.log.Error("删开口失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "删除失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

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
		if math.Abs(w.ArcExtent) < 0.5 {
			edges = append(edges, control.Wall{A: w.AID.String(), B: w.BID.String()})
			continue
		}
		// 弧墙先离散成折线再进推导：房间多边形因此是折线近似（≤5° 一段）。
		// 采样点用「墙 id + 序号」当临时端点，不落库，墙一改哈希自然就变。
		prev := w.AID.String()
		for i, ap := range control.ArcPolyline(control.Point{X: w.AX, Y: w.AY}, control.Point{X: w.BX, Y: w.BY}, w.ArcExtent) {
			id := fmt.Sprintf("%s#arc%d", w.ID.String(), i)
			points = append(points, control.Point{ID: id, X: ap.X, Y: ap.Y})
			edges = append(edges, control.Wall{A: prev, B: id})
			prev = id
		}
		edges = append(edges, control.Wall{A: prev, B: w.BID.String()})
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

	openings, err := s.plan.LoadOpenings(ctx)
	if err != nil {
		return gen.Plan{}, err
	}
	plan.Openings = make([]gen.Opening, 0, len(openings))
	for _, o := range openings {
		plan.Openings = append(plan.Openings, gen.Opening{
			Id:     o.ID,
			WallId: o.WallID,
			Kind:   gen.OpeningKind(o.Kind),
			Offset: o.Offset,
			Width:  o.Width,
			Height: o.Height,
			Sill:   o.Sill,
		})
	}

	// 底图元数据随户型一次返回；图片字节走单独的路由，避免每次 GET /api/plan
	// 都拖着一张几 MB 的图片。
	underlay, err := s.plan.LoadUnderlay(ctx)
	if err != nil {
		return gen.Plan{}, err
	}
	if underlay != nil {
		u := gen.Underlay{
			ContentType: gen.UnderlayContentType(underlay.ContentType),
			WidthPx:     underlay.WidthPx,
			HeightPx:    underlay.HeightPx,
			Sha256:      underlay.Sha256,
			CmPerPx:     underlay.CmPerPx,
			OffsetXCm:   underlay.OffsetXCm,
			OffsetYCm:   underlay.OffsetYCm,
			RotationDeg: gen.UnderlayRotationDeg(underlay.RotationDeg),
			Opacity:     underlay.Opacity,
			Visible:     underlay.Visible,
			UpdatedAt:   underlay.UpdatedAt,
		}
		plan.Underlay = &u
	}

	// 失配的名字：墙改过之后哈希变了，旧名字不该悄悄消失，也不能自动挂到
	// 形状不同的房间上——单列出来让用户自己决定重命名还是清掉。
	live := make(map[string]struct{}, len(derived.Rings))
	for _, ring := range derived.Rings {
		live[ring.Hash] = struct{}{}
	}
	plan.StaleRooms = make([]gen.StaleRoom, 0, len(names))
	for _, n := range names {
		if _, ok := live[n.Hash]; ok {
			continue
		}
		stale := gen.StaleRoom{Hash: n.Hash, Name: n.Name}
		if n.Kind != "" {
			k := gen.StaleRoomKind(n.Kind)
			stale.Kind = &k
		}
		plan.StaleRooms = append(plan.StaleRooms, stale)
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
			ArcExtent: w.ArcExtent,
		})
	}
	return plan, nil
}

// UpdateRoomName 给房间起名或改用途。
func (s *Server) UpdateRoomName(w http.ResponseWriter, r *http.Request, hash string) {
	var body gen.UpdateRoomNameJSONRequestBody
	if !decodeJSONBody(w, r, &body) {
		return
	}
	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "房间名不能为空")
		return
	}
	if utf8.RuneCountInString(name) > 24 {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "房间名最多 24 个字")
		return
	}
	kind := string(gen.RoomKindLiving)
	if body.Kind != nil {
		kind = string(*body.Kind)
	}
	ok, err := s.roomExists(r.Context(), hash)
	if err != nil {
		s.log.Error("读取户型失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取户型失败")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, gen.NOTFOUND, roomGoneMessage)
		return
	}
	if err := s.plan.UpsertRoomName(r.Context(), hash, name, kind); err != nil {
		s.log.Error("保存房间名失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "保存房间名失败")
		return
	}
	s.writePlan(w, r)
}

// ClearRoomName 清掉名字。幂等：没有名字、哈希已失配都返回 204。
// 不校验「这个哈希是不是当前房间」——失配名字的清理入口正是这里。
func (s *Server) ClearRoomName(w http.ResponseWriter, r *http.Request, hash string) {
	if _, err := s.plan.DeleteRoomName(r.Context(), hash); err != nil {
		s.log.Error("清除房间名失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "清除房间名失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// roomExists 校验哈希确实属于当前户型推导出来的某个房间。
// 不做这层校验的话，前端拿着过期哈希也能写进一行永远不会被读到的名字。
func (s *Server) roomExists(ctx context.Context, hash string) (bool, error) {
	plan, err := s.readPlan(ctx)
	if err != nil {
		return false, err
	}
	for _, room := range plan.Rooms {
		if room.Hash == hash {
			return true, nil
		}
	}
	return false, nil
}

// writePlan 回写最新整份户型——起名之后前端可以直接用返回值刷新，不用再取一次。
func (s *Server) writePlan(w http.ResponseWriter, r *http.Request) {
	plan, err := s.readPlan(r.Context())
	if err != nil {
		s.log.Error("读取户型失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取户型失败")
		return
	}
	writeJSON(w, http.StatusOK, plan)
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
	if math.Abs(in.ArcExtent) >= 180 {
		return "弧度必须在 −180° 到 180° 之间", false
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
		arc := 0.0
		if wall.ArcExtent != nil {
			arc = *wall.ArcExtent
		}
		if msg, ok := validateWall(WallInput{
			AX: wall.Ax, AY: wall.Ay, BX: wall.Bx, BY: wall.By, Thickness: thickness, ArcExtent: arc,
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
			ArcExtent: arc,
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
