package api

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/control"
	itemstore "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store"
	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

type stubCommandHandler struct {
	simCfg control.SimulatorConfig
}

func (s *stubCommandHandler) Handle(_ context.Context, _ control.Command) (control.CommandResult, error) {
	return control.CommandResult{}, nil
}
func (s *stubCommandHandler) ListLogs(_ context.Context, _ int32) ([]storegen.ListCommandLogsRow, error) {
	return nil, nil
}
func (s *stubCommandHandler) ListStates(_ context.Context, _ uuid.UUID) ([]storegen.CapabilityState, error) {
	return nil, nil
}
func (s *stubCommandHandler) ListAllStates(_ context.Context) ([]storegen.CapabilityState, error) {
	return nil, nil
}
func (s *stubCommandHandler) ListHistory(_ context.Context, _ uuid.UUID, _ *string, _ int32) ([]storegen.StateHistory, error) {
	return nil, nil
}
func (s *stubCommandHandler) ListHistoryRange(_ context.Context, _ uuid.UUID, _ *string, _, _ *time.Time, _ int32) ([]storegen.StateHistory, error) {
	return nil, nil
}
func (s *stubCommandHandler) GetSimulatorConfig() control.SimulatorConfig { return s.simCfg }
func (s *stubCommandHandler) UpdateSimulatorConfig(delayMs int, failureRate float64) (control.SimulatorConfig, bool) {
	s.simCfg = control.SimulatorConfig{DelayMs: delayMs, FailureRate: failureRate}
	return s.simCfg, true
}

type stubAccountStore struct {
	byID map[openapi_types.UUID]Account
}

func (s *stubAccountStore) AccountByUsername(_ context.Context, _ Username) (Account, error) {
	return Account{}, ErrAccountNotFound
}
func (s *stubAccountStore) AccountByID(_ context.Context, id openapi_types.UUID) (Account, error) {
	if a, ok := s.byID[id]; ok {
		return a, nil
	}
	return Account{}, ErrAccountNotFound
}

type stubPlanStore struct {
	PlanStore
	rooms []RoomName
}

func (s *stubPlanStore) LoadPlan(_ context.Context) ([]PlanCorner, []PlanWall, error) {
	return nil, nil, nil
}
func (s *stubPlanStore) LoadRoomNames(_ context.Context) ([]RoomName, error) { return s.rooms, nil }
func (s *stubPlanStore) LoadVersion(_ context.Context) (int64, error)        { return 1, nil }
func (s *stubPlanStore) LoadOpenings(_ context.Context) ([]PlanOpening, error) {
	return nil, nil
}
func (s *stubPlanStore) LoadUnderlay(_ context.Context) (*PlanUnderlay, error) { return nil, nil }

type stubTypeStore struct {
	ItemTypeStore
	types map[string]ItemType
}

func (s *stubTypeStore) ListItemTypes(_ context.Context) ([]ItemType, error) {
	out := make([]ItemType, 0, len(s.types))
	for _, t := range s.types {
		out = append(out, t)
	}
	return out, nil
}
func (s *stubTypeStore) GetItemType(_ context.Context, key string) (ItemType, error) {
	if t, ok := s.types[key]; ok {
		return t, nil
	}
	return ItemType{}, ErrItemTypeNotFound
}

type stubItemStore struct {
	items []itemstore.Item
}

func (s *stubItemStore) ListItems(_ context.Context, _ itemstore.ItemFilter) ([]itemstore.Item, error) {
	return append([]itemstore.Item(nil), s.items...), nil
}
func (s *stubItemStore) GetItem(_ context.Context, id uuid.UUID) (itemstore.Item, error) {
	for _, it := range s.items {
		if it.ID == id {
			return it, nil
		}
	}
	return itemstore.Item{}, itemstore.ErrItemNotFound
}
func (s *stubItemStore) CreateItem(_ context.Context, in itemstore.ItemCreate) (itemstore.Item, error) {
	it := itemstore.Item{
		ID:       uuid.New(),
		TypeKey:  in.TypeKey,
		Kind:     "device",
		Name:     in.Name,
		RoomHash: in.RoomHash,
		X:        in.X,
		Y:        in.Y,
		Rotation: in.Rotation,
		Attrs:    json.RawMessage(`{}`),
	}
	s.items = append(s.items, it)
	return it, nil
}
func (s *stubItemStore) UpdateItem(_ context.Context, in itemstore.ItemUpdate) (itemstore.Item, error) {
	for i, it := range s.items {
		if it.ID == in.ID {
			if in.NameSet {
				it.Name = in.Name
			}
			if in.PositionSet {
				it.X, it.Y, it.Rotation = in.X, in.Y, in.Rotation
			}
			if in.RoomSet {
				h := in.RoomHash
				it.RoomHash = &h
			}
			s.items[i] = it
			return it, nil
		}
	}
	return itemstore.Item{}, itemstore.ErrItemNotFound
}
func (s *stubItemStore) DeleteItem(_ context.Context, _ uuid.UUID) (int64, error) { return 1, nil }

func newUnitServer() (*Server, *SessionCodec, openapi_types.UUID, openapi_types.UUID) {
	adminID := openapi_types.UUID(uuid.New())
	memberID := openapi_types.UUID(uuid.New())
	accts := &stubAccountStore{byID: map[openapi_types.UUID]Account{
		adminID:  {ID: adminID, Username: "zhang", Role: "admin"},
		memberID: {ID: memberID, Username: "li", Role: "member"},
	}}
	planStore := &stubPlanStore{rooms: []RoomName{{Hash: "r1", Name: "客厅", Kind: "living"}}}
	typeStore := &stubTypeStore{types: map[string]ItemType{
		"sofa": {Key: "sofa", Kind: "furniture", Name: "沙发", DefaultWidth: 180, DefaultDepth: 80},
		"ac":   {Key: "ac", Kind: "device", Name: "空调", DefaultWidth: 60, DefaultDepth: 25},
	}}
	itemStore := &stubItemStore{items: []itemstore.Item{
		{ID: uuid.New(), TypeKey: "sofa", Kind: "furniture", Name: "三人沙发", X: 100, Y: 100, Attrs: json.RawMessage(`{}`)},
	}}
	codec := NewSessionCodec("test-secret", time.Hour)
	cmdStub := &stubCommandHandler{simCfg: control.SimulatorConfig{DelayMs: 100, FailureRate: 0}}
	srv := New(accts, planStore, typeStore, itemStore, cmdStub, codec, false, "test", slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.SetSimEnv(control.NewSimEnvStore())
	return srv, codec, adminID, memberID
}

func TestItemsExportImportAndPermissions(t *testing.T) {
	srv, codec, adminID, memberID := newUnitServer()
	router := srv.Router()
	adminCookie := &http.Cookie{Name: SessionCookieName, Value: codec.Sign(adminID)}
	memberCookie := &http.Cookie{Name: SessionCookieName, Value: codec.Sign(memberID)}

	// member 导出应 403
	req := httptest.NewRequest(http.MethodGet, "/api/items/export?format=csv", nil)
	req.AddCookie(memberCookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member export expected 403, got %d", rec.Code)
	}

	// admin 导出 CSV（含 UTF-8 BOM）
	req = httptest.NewRequest(http.MethodGet, "/api/items/export?format=csv", nil)
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.HasPrefix(rec.Body.String(), "\uFEFFname,typeKey,roomName,x,y,rotation") {
		t.Fatalf("csv export unexpected: status=%d body=%q", rec.Code, rec.Body.String())
	}

	// admin 导入 CSV：1 条同名跳过 + 1 条新建 + 1 条不存在类型报错
	csvPayload := "name,typeKey,roomName,x,y,rotation\n三人沙发,sofa,客厅,150,150,0\n客厅空调,ac,客厅,200,50,0\n坏物品,unknown_type,客厅,10,10,0\n"
	body, _ := json.Marshal(importRequest{Format: "csv", Mode: "skip", Content: csvPayload})
	req = httptest.NewRequest(http.MethodPost, "/api/items/import", strings.NewReader(string(body)))
	req.Header.Set("X-Requested-With", "fetch")
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("import expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var report importReport
	if err := json.Unmarshal(rec.Body.Bytes(), &report); err != nil {
		t.Fatalf("decode import report: %v", err)
	}
	if report.Created != 1 || report.Skipped != 1 || len(report.Errors) != 1 {
		t.Fatalf("unexpected skip report: %+v", report)
	}

	// overwrite 模式覆盖同名
	body, _ = json.Marshal(importRequest{Format: "csv", Mode: "overwrite", Content: "name,typeKey,roomName,x,y,rotation\n三人沙发,sofa,客厅,180,190,90\n"})
	req = httptest.NewRequest(http.MethodPost, "/api/items/import", strings.NewReader(string(body)))
	req.Header.Set("X-Requested-With", "fetch")
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	var overwriteReport importReport
	_ = json.Unmarshal(rec.Body.Bytes(), &overwriteReport)
	if overwriteReport.Updated != 1 || overwriteReport.Created != 0 || overwriteReport.Errors == nil {
		t.Fatalf("unexpected overwrite report (errors must be non-nil slice): %+v raw=%s", overwriteReport, rec.Body.String())
	}
}

func TestSimConfigAndWallSnapshot(t *testing.T) {
	srv, codec, adminID, memberID := newUnitServer()
	router := srv.Router()
	adminCookie := &http.Cookie{Name: SessionCookieName, Value: codec.Sign(adminID)}
	memberCookie := &http.Cookie{Name: SessionCookieName, Value: codec.Sign(memberID)}

	// 匿名访问 /api/wall/snapshot 应 200；匿名访问 /api/items 应 401
	recWall := httptest.NewRecorder()
	router.ServeHTTP(recWall, httptest.NewRequest(http.MethodGet, "/api/wall/snapshot", nil))
	if recWall.Code != http.StatusOK {
		t.Fatalf("anonymous /api/wall/snapshot expected 200, got %d: %s", recWall.Code, recWall.Body.String())
	}
	recItems := httptest.NewRecorder()
	router.ServeHTTP(recItems, httptest.NewRequest(http.MethodGet, "/api/items", nil))
	if recItems.Code != http.StatusUnauthorized {
		t.Fatalf("anonymous /api/items expected 401, got %d", recItems.Code)
	}

	// member 修改 /api/sim/config 应 403
	req := httptest.NewRequest(http.MethodPut, "/api/sim/config", strings.NewReader(`{"delayMs":200,"failureRate":0.5}`))
	req.Header.Set("X-Requested-With", "fetch")
	req.AddCookie(memberCookie)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("member sim config PUT expected 403, got %d", rec.Code)
	}

	// admin 越界 failureRate 应 400
	req = httptest.NewRequest(http.MethodPut, "/api/sim/config", strings.NewReader(`{"delayMs":200,"failureRate":1.5}`))
	req.Header.Set("X-Requested-With", "fetch")
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid failureRate expected 400, got %d", rec.Code)
	}

	// admin 合法更新应 200
	req = httptest.NewRequest(http.MethodPut, "/api/sim/config", strings.NewReader(`{"delayMs":250,"failureRate":0.3}`))
	req.Header.Set("X-Requested-With", "fetch")
	req.AddCookie(adminCookie)
	rec = httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid sim config PUT expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
}
