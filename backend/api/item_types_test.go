package api_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api"
)

type itemTypeResponse struct {
	Key                 string `json:"key"`
	Kind                string `json:"kind"`
	Name                string `json:"name"`
	DefaultWidth        float64 `json:"defaultWidth"`
	DefaultDepth        float64 `json:"defaultDepth"`
	CapabilitiesVersion int     `json:"capabilitiesVersion"`
	Capabilities        []struct {
		Key      string `json:"key"`
		Type     string `json:"type"`
		Writable bool   `json:"writable"`
		Default  any    `json:"default"`
	} `json:"capabilities"`
}

func validItemTypeBody(key string) string {
	return fmt.Sprintf(`{
	  "key": %q,
	  "kind": "device",
	  "name": "智能晾衣架",
	  "defaultWidth": 120,
	  "defaultDepth": 40,
	  "driver": "simulator",
	  "capabilities": [
	    {"key":"on","name":"开关","type":"bool","writable":true,"history":true,"default":false},
	    {"key":"height","name":"升降高度","type":"number","unit":"cm","min":0,"max":200,"step":1,"writable":true,"history":true,"default":0}
	  ]
	}`, key)
}

func cleanupItemType(t *testing.T, h http.Handler, cookie *http.Cookie, key string) {
	t.Helper()
	do(t, h, http.MethodDelete, "/api/item-types/"+key, "", cookie)
}

func TestCreateItemTypeRequiresAdmin(t *testing.T) {
	h := newTestServer(t)
	member := loginAs(t, h, "li")

	w := do(t, h, http.MethodPost, "/api/item-types", validItemTypeBody("test_member_rack"), member)
	if w.Code != http.StatusForbidden {
		t.Fatalf("member 新建类型应 403，实际 %d：%s", w.Code, w.Body.String())
	}
	if code := errorCode(t, w); code != "FORBIDDEN" {
		t.Fatalf("错误码应为 FORBIDDEN，实际 %q", code)
	}
}

func TestCreateItemTypeAppearsInList(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")
	const key = "test_smart_rack"
	cleanupItemType(t, h, admin, key)
	t.Cleanup(func() { cleanupItemType(t, h, admin, key) })

	w := do(t, h, http.MethodPost, "/api/item-types", validItemTypeBody(key), admin)
	if w.Code != http.StatusCreated {
		t.Fatalf("新建类型应 201，实际 %d：%s", w.Code, w.Body.String())
	}
	var created itemTypeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("创建响应不是合法 JSON：%v", err)
	}
	if created.Key != key || created.Kind != "device" || created.CapabilitiesVersion != 1 {
		t.Fatalf("创建响应不对：%+v", created)
	}
	if created.DefaultWidth != 120 || created.DefaultDepth != 40 {
		t.Fatalf("默认尺寸应保留，实际 %+v", created)
	}

	list := do(t, h, http.MethodGet, "/api/item-types", "", admin)
	var types []itemTypeResponse
	if err := json.Unmarshal(list.Body.Bytes(), &types); err != nil {
		t.Fatalf("列表不是合法 JSON：%v", err)
	}
	for _, typ := range types {
		if typ.Key != key {
			continue
		}
		if len(typ.Capabilities) != 2 {
			t.Fatalf("新建类型应有 2 个能力项，实际 %+v", typ.Capabilities)
		}
		if typ.Capabilities[0].Key != "on" || typ.Capabilities[0].Type != "bool" || typ.Capabilities[0].Default != false {
			t.Fatalf("bool 能力项没有按契约往返：%+v", typ.Capabilities[0])
		}
		if typ.Capabilities[1].Key != "height" || typ.Capabilities[1].Type != "number" {
			t.Fatalf("number 能力项没有按契约往返：%+v", typ.Capabilities[1])
		}
		return
	}
	t.Fatalf("新建的 %s 没有出现在列表里", key)
}

func TestCreateItemTypeRejectsInvalidDefinitions(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")

	cases := []struct {
		name string
		body string
	}{
		{
			name: "重复能力键",
			body: `{"key":"test_bad","kind":"device","name":"坏类型","capabilities":[
			  {"key":"on","name":"开关","type":"bool","writable":true,"history":true},
			  {"key":"on","name":"再一个开关","type":"bool","writable":true,"history":true}]}`,
		},
		{
			name: "重复能力名称",
			body: `{"key":"test_bad","kind":"device","name":"坏类型","capabilities":[
			  {"key":"on","name":"开关","type":"bool","writable":true,"history":true},
			  {"key":"power","name":"开关","type":"bool","writable":true,"history":true}]}`,
		},
		{
			name: "枚举没有选项",
			body: `{"key":"test_bad","kind":"device","name":"坏类型","capabilities":[
			  {"key":"mode","name":"模式","type":"enum","writable":true,"history":true}]}`,
		},
		{
			name: "最小值大于最大值",
			body: `{"key":"test_bad","kind":"device","name":"坏类型","capabilities":[
			  {"key":"level","name":"档位","type":"number","min":10,"max":1,"writable":true,"history":true}]}`,
		},
		{
			name: "默认值越界",
			body: `{"key":"test_bad","kind":"device","name":"坏类型","capabilities":[
			  {"key":"level","name":"档位","type":"number","min":0,"max":10,"default":11,"writable":true,"history":true}]}`,
		},
		{
			name: "布尔带选项",
			body: `{"key":"test_bad","kind":"device","name":"坏类型","capabilities":[
			  {"key":"on","name":"开关","type":"bool","options":[{"value":"a","label":"A"}],"writable":true,"history":true}]}`,
		},
		{
			name: "key 不是小写蛇形",
			body: `{"key":"TestBad","kind":"device","name":"坏类型","capabilities":[]}`,
		},
		{
			name: "家具带 driver",
			body: `{"key":"test_bad","kind":"furniture","name":"坏家具","driver":"simulator","capabilities":[]}`,
		},
		{
			name: "名称为空",
			body: `{"key":"test_bad","kind":"device","name":"  ","capabilities":[]}`,
		},
		{
			name: "默认尺寸小于 1",
			body: `{"key":"test_bad","kind":"device","name":"坏类型","defaultWidth":0.5,"capabilities":[]}`,
		},
		{
			name: "能力名称超过 64 字符",
			body: fmt.Sprintf(`{"key":"test_bad","kind":"device","name":"坏类型","capabilities":[
			  {"key":"level","name":%q,"type":"bool","writable":true,"history":true}]}`, strings.Repeat("长", 65)),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := do(t, h, http.MethodPost, "/api/item-types", tc.body, admin)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("应 400，实际 %d：%s", w.Code, w.Body.String())
			}
			if code := errorCode(t, w); code != "VALIDATION_FAILED" {
				t.Fatalf("错误码应为 VALIDATION_FAILED，实际 %q", code)
			}
		})
	}
}

func TestCreateItemTypeRejectsDuplicateKey(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")
	const key = "test_duplicate_rack"
	cleanupItemType(t, h, admin, key)
	t.Cleanup(func() { cleanupItemType(t, h, admin, key) })

	first := do(t, h, http.MethodPost, "/api/item-types", validItemTypeBody(key), admin)
	if first.Code != http.StatusCreated {
		t.Fatalf("第一次创建应 201，实际 %d：%s", first.Code, first.Body.String())
	}
	second := do(t, h, http.MethodPost, "/api/item-types", validItemTypeBody(key), admin)
	if second.Code != http.StatusConflict {
		t.Fatalf("重复 key 应 409，实际 %d：%s", second.Code, second.Body.String())
	}
	if code := errorCode(t, second); code != "CONFLICT" {
		t.Fatalf("错误码应为 CONFLICT，实际 %q", code)
	}
}

func TestUpdateItemTypeBumpsVersionOnlyWhenCapabilitiesChange(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")
	const key = "test_update_rack"
	cleanupItemType(t, h, admin, key)
	t.Cleanup(func() { cleanupItemType(t, h, admin, key) })

	create := do(t, h, http.MethodPost, "/api/item-types", validItemTypeBody(key), admin)
	if create.Code != http.StatusCreated {
		t.Fatalf("创建应 201，实际 %d：%s", create.Code, create.Body.String())
	}

	rename := do(t, h, http.MethodPatch, "/api/item-types/"+key, `{"name":"晾衣架改名"}`, admin)
	if rename.Code != http.StatusOK {
		t.Fatalf("改名应 200，实际 %d：%s", rename.Code, rename.Body.String())
	}
	var afterRename itemTypeResponse
	if err := json.Unmarshal(rename.Body.Bytes(), &afterRename); err != nil {
		t.Fatalf("改名响应不是 JSON：%v", err)
	}
	if afterRename.Name != "晾衣架改名" || afterRename.CapabilitiesVersion != 1 {
		t.Fatalf("只改名字不应递增能力版本：%+v", afterRename)
	}

	change := do(t, h, http.MethodPatch, "/api/item-types/"+key, `{"capabilitiesVersion":1,"capabilities":[
	  {"key":"on","name":"开关","type":"bool","writable":true,"history":true,"default":false},
	  {"key":"height","name":"升降高度","type":"number","unit":"cm","min":0,"max":220,"step":1,"writable":true,"history":true,"default":0}
	]}`, admin)
	if change.Code != http.StatusOK {
		t.Fatalf("改能力应 200，实际 %d：%s", change.Code, change.Body.String())
	}
	var afterChange itemTypeResponse
	if err := json.Unmarshal(change.Body.Bytes(), &afterChange); err != nil {
		t.Fatalf("改能力响应不是 JSON：%v", err)
	}
	if afterChange.CapabilitiesVersion != 2 {
		t.Fatalf("能力结构变化后版本应为 2，实际 %d", afterChange.CapabilitiesVersion)
	}

	stale := do(t, h, http.MethodPatch, "/api/item-types/"+key, `{"name":"过期写入","capabilitiesVersion":1}`, admin)
	if stale.Code != http.StatusConflict {
		t.Fatalf("过期版本应 409，实际 %d：%s", stale.Code, stale.Body.String())
	}
	if code := errorCode(t, stale); code != "CONFLICT" {
		t.Fatalf("错误码应为 CONFLICT，实际 %q", code)
	}

	missingVersion := do(t, h, http.MethodPatch, "/api/item-types/"+key, `{"capabilities":[
	  {"key":"on","name":"开关","type":"bool","writable":true,"history":true,"default":false}
	]}`, admin)
	if missingVersion.Code != http.StatusBadRequest {
		t.Fatalf("改能力项不带版本应 400，实际 %d：%s", missingVersion.Code, missingVersion.Body.String())
	}
}

func TestUpdateItemTypeCanClearIcon(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")
	const key = "test_icon_rack"
	cleanupItemType(t, h, admin, key)
	t.Cleanup(func() { cleanupItemType(t, h, admin, key) })

	body := strings.Replace(validItemTypeBody(key), `"driver": "simulator",`, `"driver": "simulator", "icons": "lamp",`, 1)
	create := do(t, h, http.MethodPost, "/api/item-types", body, admin)
	if create.Code != http.StatusCreated {
		t.Fatalf("创建应 201，实际 %d：%s", create.Code, create.Body.String())
	}

	clear := do(t, h, http.MethodPatch, "/api/item-types/"+key, `{"icons":""}`, admin)
	if clear.Code != http.StatusOK {
		t.Fatalf("清图标应 200，实际 %d：%s", clear.Code, clear.Body.String())
	}
	var updated struct {
		Icons *string `json:"icons"`
	}
	if err := json.Unmarshal(clear.Body.Bytes(), &updated); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if updated.Icons != nil && *updated.Icons != "" {
		t.Fatalf("图标应被清空，实际 %q", *updated.Icons)
	}
}

func TestUpdateItemTypeProtectsPublishedEnumValuesAndTypes(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")
	const key = "test_enum_rack"
	cleanupItemType(t, h, admin, key)
	t.Cleanup(func() { cleanupItemType(t, h, admin, key) })

	createBody := `{"key":"test_enum_rack","kind":"device","name":"枚举测试","capabilities":[
	  {"key":"mode","name":"模式","type":"enum","options":[{"value":"cool","label":"制冷"}],"writable":true,"history":true,"default":"cool"}
	]}`
	create := do(t, h, http.MethodPost, "/api/item-types", createBody, admin)
	if create.Code != http.StatusCreated {
		t.Fatalf("创建应 201，实际 %d：%s", create.Code, create.Body.String())
	}

	renameValue := do(t, h, http.MethodPatch, "/api/item-types/"+key, `{"capabilitiesVersion":1,"capabilities":[
	  {"key":"mode","name":"模式","type":"enum","options":[{"value":"cooling","label":"制冷"}],"writable":true,"history":true,"default":"cooling"}
	]}`, admin)
	if renameValue.Code != http.StatusBadRequest {
		t.Fatalf("改枚举 value 应 400，实际 %d：%s", renameValue.Code, renameValue.Body.String())
	}

	changeType := do(t, h, http.MethodPatch, "/api/item-types/"+key, `{"capabilitiesVersion":1,"capabilities":[
	  {"key":"mode","name":"模式","type":"number","min":0,"max":1,"writable":true,"history":true,"default":0}
	]}`, admin)
	if changeType.Code != http.StatusBadRequest {
		t.Fatalf("改能力项类型应 400，实际 %d：%s", changeType.Code, changeType.Body.String())
	}

	addOption := do(t, h, http.MethodPatch, "/api/item-types/"+key, `{"capabilitiesVersion":1,"capabilities":[
	  {"key":"mode","name":"模式","type":"enum","options":[{"value":"cool","label":"制冷"},{"value":"heat","label":"制热"}],"writable":true,"history":true,"default":"cool"}
	]}`, admin)
	if addOption.Code != http.StatusOK {
		t.Fatalf("新增枚举选项应 200，实际 %d：%s", addOption.Code, addOption.Body.String())
	}
	var updated itemTypeResponse
	if err := json.Unmarshal(addOption.Body.Bytes(), &updated); err != nil {
		t.Fatalf("响应不是 JSON：%v", err)
	}
	if updated.CapabilitiesVersion != 2 {
		t.Fatalf("新增枚举选项后版本应为 2，实际 %d", updated.CapabilitiesVersion)
	}
}

func TestDeleteItemTypeRejectsReferencedType(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")

	referenced := do(t, h, http.MethodDelete, "/api/item-types/ceiling_light", "", admin)
	if referenced.Code != http.StatusConflict {
		t.Fatalf("被引用的类型应 409，实际 %d：%s", referenced.Code, referenced.Body.String())
	}
	if code := errorCode(t, referenced); code != "CONFLICT" {
		t.Fatalf("错误码应为 CONFLICT，实际 %q", code)
	}

	missing := do(t, h, http.MethodDelete, "/api/item-types/no_such_type", "", admin)
	if missing.Code != http.StatusNotFound {
		t.Fatalf("不存在的类型应 404，实际 %d", missing.Code)
	}

	const key = "test_delete_rack"
	cleanupItemType(t, h, admin, key)
	t.Cleanup(func() { cleanupItemType(t, h, admin, key) })
	create := do(t, h, http.MethodPost, "/api/item-types", validItemTypeBody(key), admin)
	if create.Code != http.StatusCreated {
		t.Fatalf("创建应 201，实际 %d：%s", create.Code, create.Body.String())
	}
	deleted := do(t, h, http.MethodDelete, "/api/item-types/"+key, "", admin)
	if deleted.Code != http.StatusNoContent {
		t.Fatalf("删除未被引用的类型应 204，实际 %d：%s", deleted.Code, deleted.Body.String())
	}
}

func TestCreateItemTypeRejectsTrailingJSON(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")
	body := validItemTypeBody("test_trailing") + `{}`
	w := do(t, h, http.MethodPost, "/api/item-types", body, admin)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("尾随第二个 JSON 值应 400，实际 %d：%s", w.Code, w.Body.String())
	}
	if code := errorCode(t, w); code != "VALIDATION_FAILED" {
		t.Fatalf("错误码应为 VALIDATION_FAILED，实际 %q", code)
	}
}

func TestItemTypesRequireSession(t *testing.T) {
	h := newTestServer(t)
	w := do(t, h, http.MethodGet, "/api/item-types", "", nil)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("未登录读类型列表应 401，实际 %d", w.Code)
	}
}

func TestCapabilityOptionAcceptsLegacyString(t *testing.T) {
	var capabilities []api.Capability
	raw := `[{"key":"mode","name":"模式","type":"enum","writable":true,"history":true,"options":["cool"]}]`
	if err := json.Unmarshal([]byte(raw), &capabilities); err != nil {
		t.Fatalf("旧 seed 的 string options 应能读取：%v", err)
	}
	if len(capabilities) != 1 || len(capabilities[0].Options) != 1 {
		t.Fatalf("旧 options 没有正确解码：%+v", capabilities)
	}
	option := capabilities[0].Options[0]
	if option.Value != "cool" || option.Label != "cool" {
		t.Fatalf("旧 options 应同时作为 value 与 label，实际 %+v", option)
	}
}

func TestUpdateItemTypeCanRemoveCapability(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")
	const key = "test_stable_rack"
	cleanupItemType(t, h, admin, key)
	t.Cleanup(func() { cleanupItemType(t, h, admin, key) })

	create := do(t, h, http.MethodPost, "/api/item-types", validItemTypeBody(key), admin)
	if create.Code != http.StatusCreated {
		t.Fatalf("创建应 201，实际 %d：%s", create.Code, create.Body.String())
	}

	// SRS FR-2：删除能力项时历史值保留但不可见。类型元数据可以去掉 on，capability_state 不动。
	remove := do(t, h, http.MethodPatch, "/api/item-types/"+key, `{"capabilitiesVersion":1,"capabilities":[
	  {"key":"height","name":"升降高度","type":"number","unit":"cm","min":0,"max":200,"step":1,"writable":true,"history":true,"default":0}
	]}`, admin)
	if remove.Code != http.StatusOK {
		t.Fatalf("删除能力项应 200，实际 %d：%s", remove.Code, remove.Body.String())
	}
	var updated itemTypeResponse
	if err := json.Unmarshal(remove.Body.Bytes(), &updated); err != nil {
		t.Fatalf("更新响应不是 JSON：%v", err)
	}
	if updated.CapabilitiesVersion != 2 || len(updated.Capabilities) != 1 {
		t.Fatalf("删除能力项后应为 v2、1 项，实际 %+v", updated)
	}
}

// loginAs 用 seed 里的演示账号换一个会话 Cookie。
func loginAs(t *testing.T, h http.Handler, username string) *http.Cookie {
	t.Helper()
	w := do(t, h, http.MethodPost, "/api/auth/login",
		`{"username":"`+username+`","password":"demo1234"}`, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("登录 %s 应 200，实际 %d：%s", username, w.Code, w.Body.String())
	}
	return sessionCookie(t, w)
}

func TestListItemTypesShowsSeededTypes(t *testing.T) {
	h := newTestServer(t)
	cookie := loginAs(t, h, "zhang")

	w := do(t, h, http.MethodGet, "/api/item-types", "", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("类型列表应 200，实际 %d：%s", w.Code, w.Body.String())
	}

	var types []struct {
		Key                 string `json:"key"`
		Kind                string `json:"kind"`
		Name                string `json:"name"`
		CapabilitiesVersion int    `json:"capabilitiesVersion"`
		Capabilities        []struct {
			Key     string `json:"key"`
			Type    string `json:"type"`
			Options []struct {
				Value string `json:"value"`
				Label string `json:"label"`
			} `json:"options"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &types); err != nil {
		t.Fatalf("类型列表不是合法 JSON：%v (%s)", err, w.Body.String())
	}
	if len(types) < 12 {
		t.Fatalf("seed 至少应返回 12 个类型，实际 %d", len(types))
	}

	byKey := make(map[string]int, len(types))
	for i, typ := range types {
		byKey[typ.Key] = i
	}
	light, ok := byKey["ceiling_light"]
	if !ok {
		t.Fatal("内置类型里没有 ceiling_light")
	}
	if types[light].Kind != "device" || types[light].CapabilitiesVersion < 1 {
		t.Fatalf("ceiling_light 的 kind/version 不对：%+v", types[light])
	}

	ac, ok := byKey["ac"]
	if !ok {
		t.Fatal("内置类型里没有 ac")
	}
	var modeFound bool
	for _, capability := range types[ac].Capabilities {
		if capability.Key != "mode" {
			continue
		}
		modeFound = true
		if capability.Type != "enum" {
			t.Fatalf("mode 应为 enum，实际 %q", capability.Type)
		}
		if len(capability.Options) == 0 || capability.Options[0].Value != "cool" || capability.Options[0].Label != "制冷" {
			t.Fatalf("mode 的 options 应为带中文 label 的对象，实际 %+v", capability.Options)
		}
	}
	if !modeFound {
		t.Fatal("ac 的能力项里没有 mode")
	}
}
