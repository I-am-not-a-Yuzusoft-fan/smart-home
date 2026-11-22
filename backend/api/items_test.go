package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const seedRoomHash = "2a6c15cc655eea79"

type itemResponse struct {
	ID       string         `json:"id"`
	TypeKey  string         `json:"typeKey"`
	Kind     string         `json:"kind"`
	Name     string         `json:"name"`
	RoomHash *string        `json:"roomHash"`
	RoomName *string        `json:"roomName"`
	X        float64        `json:"x"`
	Y        float64        `json:"y"`
	Rotation float64        `json:"rotation"`
	Status   *string        `json:"status"`
	Attrs    map[string]any `json:"attrs"`
}

func listItems(t *testing.T, h http.Handler, cookie *http.Cookie, query string) []itemResponse {
	t.Helper()
	w := do(t, h, http.MethodGet, "/api/items"+query, "", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("物品列表应 200，实际 %d：%s", w.Code, w.Body.String())
	}
	var items []itemResponse
	if err := json.Unmarshal(w.Body.Bytes(), &items); err != nil {
		t.Fatalf("物品列表不是合法 JSON：%v (%s)", err, w.Body.String())
	}
	return items
}

func createItem(t *testing.T, h http.Handler, cookie *http.Cookie, body string) itemResponse {
	t.Helper()
	w := do(t, h, http.MethodPost, "/api/items", body, cookie)
	if w.Code != http.StatusCreated {
		t.Fatalf("新建物品应 201，实际 %d：%s", w.Code, w.Body.String())
	}
	var item itemResponse
	if err := json.Unmarshal(w.Body.Bytes(), &item); err != nil {
		t.Fatalf("新建响应不是 JSON：%v", err)
	}
	return item
}

func TestListItemsShowsSeedAndFilters(t *testing.T) {
	h := newTestServer(t)
	cookie := loginAs(t, h, "zhang")

	all := listItems(t, h, cookie, "")
	if len(all) < 8 {
		t.Fatalf("seed 至少应有 8 件物品，实际 %d", len(all))
	}
	for _, item := range all {
		if item.ID == "" || item.TypeKey == "" || item.Kind == "" || item.Name == "" {
			t.Fatalf("物品缺少必填字段：%+v", item)
		}
	}

	furniture := listItems(t, h, cookie, "?kind=furniture")
	if len(furniture) != 2 {
		t.Fatalf("seed 家具应为 2 件，实际 %d", len(furniture))
	}
	devices := listItems(t, h, cookie, "?kind=device")
	if len(devices) != 6 {
		t.Fatalf("seed 设备应为 6 件，实际 %d", len(devices))
	}
	if got := listItems(t, h, cookie, "?q=空调"); len(got) != 1 || got[0].Name != "客厅空调" {
		t.Fatalf("关键字筛选不对：%+v", got)
	}
	if got := listItems(t, h, cookie, "?room="+seedRoomHash); len(got) < 8 {
		t.Fatalf("按房间筛选应返回 seed 物品，实际 %d", len(got))
	}
	if got := listItems(t, h, cookie, "?room=no-such-room"); len(got) != 0 {
		t.Fatalf("不存在的房间应返回空列表，实际 %+v", got)
	}

	bad := do(t, h, http.MethodGet, "/api/items?kind=robot", "", cookie)
	if bad.Code != http.StatusBadRequest {
		t.Fatalf("非法 kind 应 400，实际 %d", bad.Code)
	}
}

func TestCreateAndUpdateFurniturePersists(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")

	created := createItem(t, h, admin, `{
	  "typeKey":"sofa","name":"测试沙发","roomHash":"`+seedRoomHash+`",
	  "x":120,"y":90,"rotation":0
	}`)
	t.Cleanup(func() { do(t, h, http.MethodDelete, "/api/items/"+created.ID, "", admin) })
	if created.Kind != "furniture" || created.RoomName == nil || *created.RoomName != "客厅" {
		t.Fatalf("新建家具投影不对：%+v", created)
	}
	if created.X != 120 || created.Y != 90 {
		t.Fatalf("坐标未落库：%+v", created)
	}

	updated := do(t, h, http.MethodPatch, "/api/items/"+created.ID,
		`{"name":"测试沙发改","x":140,"y":100,"rotation":90,"roomHash":""}`, admin)
	if updated.Code != http.StatusOK {
		t.Fatalf("修改物品应 200，实际 %d：%s", updated.Code, updated.Body.String())
	}
	var after itemResponse
	if err := json.Unmarshal(updated.Body.Bytes(), &after); err != nil {
		t.Fatalf("修改响应不是 JSON：%v", err)
	}
	if after.Name != "测试沙发改" || after.X != 140 || after.Y != 100 || after.Rotation != 90 {
		t.Fatalf("修改字段未落库：%+v", after)
	}
	if after.RoomHash != nil || after.RoomName != nil {
		t.Fatalf("roomHash 传空应清除房间：%+v", after)
	}

	// 重新读列表，确认不是只改内存。
	var found *itemResponse
	for _, item := range listItems(t, h, admin, "") {
		if item.ID == created.ID {
			copy := item
			found = &copy
			break
		}
	}
	if found == nil || found.Name != "测试沙发改" || found.X != 140 || found.Y != 100 {
		t.Fatalf("列表没有反映修改：%+v", found)
	}
}

func TestCreateDeviceInitializesCapabilityState(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")

	created := createItem(t, h, admin, `{
	  "typeKey":"ceiling_light","name":"测试灯","roomHash":"`+seedRoomHash+`",
	  "x":200,"y":150
	}`)
	t.Cleanup(func() { do(t, h, http.MethodDelete, "/api/items/"+created.ID, "", admin) })

	dsn := os.Getenv("TEST_DATABASE_URL")
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("连接数据库失败：%v", err)
	}
	defer pool.Close()

	rows, err := pool.Query(context.Background(),
		`SELECT capability_key, value::text FROM capability_state WHERE item_id = $1::uuid ORDER BY capability_key`, created.ID)
	if err != nil {
		t.Fatalf("查询能力状态失败：%v", err)
	}
	defer rows.Close()
	got := map[string]string{}
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			t.Fatalf("扫描能力状态失败：%v", err)
		}
		got[key] = value
	}
	if got["on"] != "false" || got["brightness"] != "0" || got["color_temp"] != "4000" {
		t.Fatalf("设备默认能力值不对：%+v", got)
	}

	deleted := do(t, h, http.MethodDelete, "/api/items/"+created.ID, "", admin)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("删除物品应 204，实际 %d", deleted.Code)
	}
	var remaining int
	if err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM capability_state WHERE item_id = $1::uuid`, created.ID).Scan(&remaining); err != nil {
		t.Fatalf("统计能力状态失败：%v", err)
	}
	if remaining != 0 {
		t.Fatalf("删除物品后 capability_state 应级联清空，实际 %d", remaining)
	}
}

func TestCreateItemRejectsBadInput(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")

	cases := []string{
		`{"typeKey":"no_such_type","name":"x","x":0,"y":0}`,
		`{"typeKey":"sofa","name":"","x":0,"y":0}`,
		`{"typeKey":"sofa","name":"x","x":-1,"y":0}`,
		`{"typeKey":"sofa","name":"x","x":6000,"y":0}`,
		`{"typeKey":"sofa","name":"x","x":0,"y":0,"roomHash":"no-such-room"}`,
	}
	for _, body := range cases {
		w := do(t, h, http.MethodPost, "/api/items", body, admin)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("非法输入应 400，实际 %d：%s (%s)", w.Code, w.Body.String(), body)
		}
	}
}

func TestUpdateAndDeleteMissingItem(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")
	missing := "00000000-0000-4000-8000-00000000dead"

	w := do(t, h, http.MethodPatch, "/api/items/"+missing, `{"name":"x"}`, admin)
	if w.Code != http.StatusNotFound {
		t.Fatalf("修改不存在物品应 404，实际 %d", w.Code)
	}
	w = do(t, h, http.MethodDelete, "/api/items/"+missing, "", admin)
	if w.Code != http.StatusNotFound {
		t.Fatalf("删除不存在物品应 404，实际 %d", w.Code)
	}
}

// 未知字段与类型错误要给出可操作的文案，而不是笼统的「不是合法的 JSON」。
func TestCreateItemReportsUnknownField(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")

	w := do(t, h, http.MethodPost, "/api/items", `{"typeKey":"sofa","name":"x","x":0,"y":0,"nope":1}`, admin)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("未知字段应 400，实际 %d：%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "不支持的字段") || !strings.Contains(w.Body.String(), "nope") {
		t.Fatalf("文案应指出具体字段，实际：%s", w.Body.String())
	}
}

func TestUpdateItemReportsUnknownField(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")
	item := createItem(t, h, admin, `{"typeKey":"sofa","name":"x","x":0,"y":0}`)
	t.Cleanup(func() { do(t, h, http.MethodDelete, "/api/items/"+item.ID, "", admin) })

	// typeKey 不在 ItemUpdate 里，改类型必须走删除重建。
	w := do(t, h, http.MethodPatch, "/api/items/"+item.ID, `{"typeKey":"ac"}`, admin)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("未知字段应 400，实际 %d：%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "不支持的字段") || !strings.Contains(w.Body.String(), "typeKey") {
		t.Fatalf("文案应指出具体字段，实际：%s", w.Body.String())
	}
}

func TestCreateItemReportsMalformedJSON(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")

	w := do(t, h, http.MethodPost, "/api/items", `{"typeKey":`, admin)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("残缺 JSON 应 400，实际 %d：%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "不是合法的 JSON") {
		t.Fatalf("文案应说明 JSON 不合法，实际：%s", w.Body.String())
	}
}

// 设备的真实状态在 capability_state 里；item.status 只对家具有语义（ADR 0002 §2.2）。
func TestCreateDeviceRejectsStatus(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")

	w := do(t, h, http.MethodPost, "/api/items",
		`{"typeKey":"ceiling_light","name":"灯","x":10,"y":10,"status":"maintenance"}`, admin)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("设备带 status 应 400，实际 %d：%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "设备不使用 status") {
		t.Fatalf("文案应说明设备不用 status，实际：%s", w.Body.String())
	}
}

func TestUpdateDeviceRejectsStatus(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")
	item := createItem(t, h, admin, `{"typeKey":"ceiling_light","name":"灯","x":10,"y":10}`)
	t.Cleanup(func() { do(t, h, http.MethodDelete, "/api/items/"+item.ID, "", admin) })

	w := do(t, h, http.MethodPatch, "/api/items/"+item.ID, `{"status":"maintenance"}`, admin)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("设备改 status 应 400，实际 %d：%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "设备不使用 status") {
		t.Fatalf("文案应说明设备不用 status，实际：%s", w.Body.String())
	}
}

// 家具仍然可以用 status。
func TestFurnitureKeepsStatus(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")
	item := createItem(t, h, admin, `{"typeKey":"sofa","name":"沙发","x":0,"y":0}`)
	t.Cleanup(func() { do(t, h, http.MethodDelete, "/api/items/"+item.ID, "", admin) })

	w := do(t, h, http.MethodPatch, "/api/items/"+item.ID, `{"status":"maintenance"}`, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("家具改 status 应 200，实际 %d：%s", w.Code, w.Body.String())
	}
}
