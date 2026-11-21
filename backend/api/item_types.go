package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api/gen"
)

// ItemType 是类型模板的领域视图。key 是稳定身份，创建后不得重命名。
type ItemType struct {
	Key                 string
	Kind                string
	Name                string
	Icons               *string
	DefaultWidth        float64
	DefaultDepth        float64
	Capabilities        []Capability
	CapabilitiesVersion int
	Driver              *string
}

// Capability 是类型级能力定义。它没有值，值在 capability_state。
// JSON tag 必须与契约一致：这份结构会原样写进 item_type.capabilities。
type Capability struct {
	Key      string             `json:"key"`
	Name     string             `json:"name"`
	Type     string             `json:"type"`
	Unit     *string            `json:"unit,omitempty"`
	Min      *float64           `json:"min,omitempty"`
	Max      *float64           `json:"max,omitempty"`
	Step     *float64           `json:"step,omitempty"`
	Options  []CapabilityOption `json:"options,omitempty"`
	Writable bool               `json:"writable"`
	Default  any                `json:"default,omitempty"`
	History  bool               `json:"history"`
}

// CapabilityOption 是枚举能力的一个选项：value 入库，label 给人看。
type CapabilityOption struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// UnmarshalJSON 兼容早期 seed 的 string[] options。
// 旧库里存的是 "cool"，新契约是 {"value":"cool","label":"cool"}；
// 读旧数据时把字符串同时当作 value 与 label，避免升级后列表直接 500。
func (o *CapabilityOption) UnmarshalJSON(data []byte) error {
	var legacy string
	if err := json.Unmarshal(data, &legacy); err == nil {
		o.Value = legacy
		o.Label = legacy
		return nil
	}
	type option CapabilityOption
	var parsed option
	if err := json.Unmarshal(data, &parsed); err != nil {
		return err
	}
	*o = CapabilityOption(parsed)
	return nil
}

var (
	// ErrItemTypeNotFound 表示 key 不存在。
	ErrItemTypeNotFound = errors.New("类型不存在")
	// ErrItemTypeExists 表示创建时 key 已存在。
	ErrItemTypeExists = errors.New("类型 key 已存在")
	// ErrItemTypeInUse 表示删除时仍有物品引用该类型（并发兜底）。
	ErrItemTypeInUse = errors.New("类型仍被引用")
	// ErrItemTypeConflict 表示乐观锁版本不匹配（并发修改）。
	ErrItemTypeConflict = errors.New("类型已被他人修改")
)

// ItemTypePatch 是逐字段 PATCH 的持久化参数。
// Set=false 表示这个字段没出现在请求里，数据库保持原值。
type ItemTypePatch struct {
	NameSet  bool
	Name     string
	IconsSet bool
	Icons    string
	WidthSet bool
	Width    float64
	DepthSet bool
	Depth    float64

	CapabilitiesSet bool
	Capabilities    []Capability

	// 乐观锁：客户端读到的 capabilitiesVersion。Set=false 表示不做检查。
	VersionSet      bool
	ExpectedVersion int

	DriverSet bool
	Driver    string
}

// ItemTypeStore 是类型模板的持久化接缝。
type ItemTypeStore interface {
	ListItemTypes(ctx context.Context) ([]ItemType, error)
	GetItemType(ctx context.Context, key string) (ItemType, error)
	CreateItemType(ctx context.Context, in ItemType) (ItemType, error)
	UpdateItemType(ctx context.Context, key string, patch ItemTypePatch) (ItemType, error)
	DeleteItemType(ctx context.Context, key string) (int64, error)
	CountItemsByType(ctx context.Context, key string) (int64, error)
	CountStateRowsForKey(ctx context.Context, typeKey, capabilityKey string) (int64, error)
}

var capabilityKeyPattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

// ListItemTypes 返回全部类型模板。成员也能读：前端只读渲染家具库。
func (s *Server) ListItemTypes(w http.ResponseWriter, r *http.Request) {
	types, err := s.itemTypes.ListItemTypes(r.Context())
	if err != nil {
		s.log.ErrorContext(r.Context(), "读取类型模板失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取类型模板失败")
		return
	}
	out := make([]gen.ItemType, 0, len(types))
	for _, typ := range types {
		out = append(out, toGenItemType(typ))
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateItemType 新建类型。只有管理员能改配置。
func (s *Server) CreateItemType(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	var body gen.ItemTypeInput
	if !decodeJSONBody(w, r, &body) {
		return
	}

	in := itemTypeFromInput(body)
	if msg, ok := validateItemType(in); !ok {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, msg)
		return
	}
	created, err := s.itemTypes.CreateItemType(r.Context(), in)
	switch {
	case err == nil:
		writeJSON(w, http.StatusCreated, toGenItemType(created))
	case errors.Is(err, ErrItemTypeExists):
		writeError(w, http.StatusConflict, gen.CONFLICT, "类型 key 已存在")
	default:
		s.log.ErrorContext(r.Context(), "创建类型模板失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "创建类型模板失败")
	}
}

// UpdateItemType 修改类型。key 不可改；capabilities 结构变化时版本由服务端递增。
func (s *Server) UpdateItemType(w http.ResponseWriter, r *http.Request, key string) {
	if !requireAdmin(w, r) {
		return
	}
	var body gen.ItemTypeUpdate
	if !decodeJSONBody(w, r, &body) {
		return
	}

	existing, err := s.itemTypes.GetItemType(r.Context(), key)
	if err != nil {
		if errors.Is(err, ErrItemTypeNotFound) {
			writeError(w, http.StatusNotFound, gen.NOTFOUND, "类型不存在")
			return
		}
		s.log.ErrorContext(r.Context(), "读取类型模板失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取类型模板失败")
		return
	}
	// 能力数组是整块替换：必须带版本，否则两个并发 PATCH 会丢能力项。
	if body.Capabilities != nil && body.CapabilitiesVersion == nil {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "修改能力项时必须携带 capabilitiesVersion")
		return
	}
	if body.CapabilitiesVersion != nil && *body.CapabilitiesVersion != existing.CapabilitiesVersion {
		writeError(w, http.StatusConflict, gen.CONFLICT, "类型已被他人修改，请刷新后重试")
		return
	}
	in := applyItemTypeUpdate(existing, body)
	if msg, ok := validateCapabilityKeysStable(existing.Capabilities, in.Capabilities); !ok {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, msg)
		return
	}
	// 删掉的能力项换一种语义再加回来，是上面那条校验拦不住的：
	// 那一步里 existing 已经不含这个 key，代码会把它当全新能力项，而 capability_state
	// 里旧值还在——一个 bool 的 true 会被当成 number 的 1 渲染出去。
	if msg, err := s.checkReusedCapabilityKeys(r.Context(), existing, in); err != nil {
		s.log.ErrorContext(r.Context(), "校验能力项 key 失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "更新类型模板失败")
		return
	} else if msg != "" {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, msg)
		return
	}
	if msg, ok := validateItemType(in); !ok {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, msg)
		return
	}
	updated, err := s.itemTypes.UpdateItemType(r.Context(), key, itemTypePatchFromGen(body))
	if err != nil {
		if errors.Is(err, ErrItemTypeConflict) {
			writeError(w, http.StatusConflict, gen.CONFLICT, "类型已被他人修改，请刷新后重试")
			return
		}
		if errors.Is(err, ErrItemTypeNotFound) {
			writeError(w, http.StatusNotFound, gen.NOTFOUND, "类型不存在")
			return
		}
		s.log.ErrorContext(r.Context(), "更新类型模板失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "更新类型模板失败")
		return
	}
	writeJSON(w, http.StatusOK, toGenItemType(updated))
}

// DeleteItemType 删除类型；仍被物品引用时拒绝。
func (s *Server) DeleteItemType(w http.ResponseWriter, r *http.Request, key string) {
	if !requireAdmin(w, r) {
		return
	}
	refs, err := s.itemTypes.CountItemsByType(r.Context(), key)
	if err != nil {
		s.log.ErrorContext(r.Context(), "统计类型引用失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "删除类型失败")
		return
	}
	if refs > 0 {
		writeError(w, http.StatusConflict, gen.CONFLICT,
			fmt.Sprintf("仍有 %d 件物品引用该类型，不能删除", refs))
		return
	}
	deleted, err := s.itemTypes.DeleteItemType(r.Context(), key)
	if errors.Is(err, ErrItemTypeInUse) {
		writeError(w, http.StatusConflict, gen.CONFLICT, "仍有物品引用该类型，不能删除")
		return
	}
	if err != nil {
		s.log.ErrorContext(r.Context(), "删除类型模板失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "删除类型失败")
		return
	}
	if deleted == 0 {
		writeError(w, http.StatusNotFound, gen.NOTFOUND, "类型不存在")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// requireAdmin 把角色检查放在 handler 里，而不是再挂一层中间件：
// 它是"某个写接口"的属性，不是整棵路由树的属性。
func requireAdmin(w http.ResponseWriter, r *http.Request) bool {
	acct, ok := AccountFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, gen.UNAUTHENTICATED, "未登录")
		return false
	}
	if acct.Role != "admin" {
		writeError(w, http.StatusForbidden, gen.FORBIDDEN, "当前账号无此权限")
		return false
	}
	return true
}

func decodeJSONBody(w http.ResponseWriter, r *http.Request, dst any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "请求体不是合法的 JSON")
		return false
	}
	// 一个请求只能有一个 JSON 值；`{...}{}` 这种尾巴不能静默放过。
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "请求体只能包含一个 JSON 值")
		return false
	}
	return true
}

func itemTypeFromInput(in gen.ItemTypeInput) ItemType {
	width, depth := 60.0, 60.0
	if in.DefaultWidth != nil {
		width = float64(*in.DefaultWidth)
	}
	if in.DefaultDepth != nil {
		depth = float64(*in.DefaultDepth)
	}
	capabilities := make([]Capability, 0, len(in.Capabilities))
	for _, c := range in.Capabilities {
		capabilities = append(capabilities, capabilityFromGen(c))
	}
	return ItemType{
		Key:          in.Key,
		Kind:         string(in.Kind),
		Name:         in.Name,
		Icons:        in.Icons,
		DefaultWidth: width,
		DefaultDepth: depth,
		Capabilities: capabilities,
		Driver:       in.Driver,
	}
}

func applyItemTypeUpdate(existing ItemType, in gen.ItemTypeUpdate) ItemType {
	out := existing
	if in.Name != nil {
		out.Name = *in.Name
	}
	if in.Icons != nil {
		out.Icons = in.Icons
	}
	if in.DefaultWidth != nil {
		out.DefaultWidth = float64(*in.DefaultWidth)
	}
	if in.DefaultDepth != nil {
		out.DefaultDepth = float64(*in.DefaultDepth)
	}
	if in.Capabilities != nil {
		out.Capabilities = make([]Capability, 0, len(*in.Capabilities))
		for _, c := range *in.Capabilities {
			out.Capabilities = append(out.Capabilities, capabilityFromGen(c))
		}
	}
	if in.Driver != nil {
		if *in.Driver == "" {
			out.Driver = nil
		} else {
			out.Driver = in.Driver
		}
	}
	return out
}

func itemTypePatchFromGen(in gen.ItemTypeUpdate) ItemTypePatch {
	patch := ItemTypePatch{}
	if in.Name != nil {
		patch.NameSet = true
		patch.Name = *in.Name
	}
	if in.Icons != nil {
		patch.IconsSet = true
		patch.Icons = *in.Icons
	}
	if in.DefaultWidth != nil {
		patch.WidthSet = true
		patch.Width = float64(*in.DefaultWidth)
	}
	if in.DefaultDepth != nil {
		patch.DepthSet = true
		patch.Depth = float64(*in.DefaultDepth)
	}
	if in.Capabilities != nil {
		patch.CapabilitiesSet = true
		patch.Capabilities = make([]Capability, 0, len(*in.Capabilities))
		for _, c := range *in.Capabilities {
			patch.Capabilities = append(patch.Capabilities, capabilityFromGen(c))
		}
	}
	if in.CapabilitiesVersion != nil {
		patch.VersionSet = true
		patch.ExpectedVersion = *in.CapabilitiesVersion
	}
	if in.Driver != nil {
		patch.DriverSet = true
		patch.Driver = *in.Driver
	}
	return patch
}

func capabilityFromGen(c gen.Capability) Capability {
	options := make([]CapabilityOption, 0)
	if c.Options != nil {
		for _, option := range *c.Options {
			options = append(options, CapabilityOption{Value: option.Value, Label: option.Label})
		}
	}
	return Capability{
		Key:      c.Key,
		Name:     c.Name,
		Type:     string(c.Type),
		Unit:     c.Unit,
		Min:      c.Min,
		Max:      c.Max,
		Step:     c.Step,
		Options:  options,
		Writable: c.Writable,
		Default:  c.Default,
		History:  c.History,
	}
}

func toGenItemType(in ItemType) gen.ItemType {
	capabilities := make([]gen.Capability, 0, len(in.Capabilities))
	for _, c := range in.Capabilities {
		options := make([]gen.CapabilityOption, 0, len(c.Options))
		for _, option := range c.Options {
			options = append(options, gen.CapabilityOption{Value: option.Value, Label: option.Label})
		}
		var optionsPtr *[]gen.CapabilityOption
		if len(options) > 0 {
			optionsPtr = &options
		}
		capabilities = append(capabilities, gen.Capability{
			Key:      c.Key,
			Name:     c.Name,
			Type:     gen.CapabilityType(c.Type),
			Unit:     c.Unit,
			Min:      c.Min,
			Max:      c.Max,
			Step:     c.Step,
			Options:  optionsPtr,
			Writable: c.Writable,
			Default:  c.Default,
			History:  c.History,
		})
	}
	return gen.ItemType{
		Key:                 in.Key,
		Kind:                gen.ItemTypeKind(in.Kind),
		Name:                in.Name,
		Icons:               in.Icons,
		DefaultWidth:        in.DefaultWidth,
		DefaultDepth:        in.DefaultDepth,
		Capabilities:        capabilities,
		CapabilitiesVersion: in.CapabilitiesVersion,
		Driver:              in.Driver,
	}
}

// validateCapabilityKeysStable 守住已发布能力项的语义：
//   - 保留下来的能力项不能改类型，也不能删除/改名已发布的枚举 value；
//   - 删除一个能力项是允许的（SRS FR-2 要求历史值保留但不可见），
//     capability_state 的行不随类型元数据删除——上层只按元数据渲染与校验，于是自然隐藏。
//   - 改名等同于「删旧 key + 加新 key」；UI 把已保存能力的 key 锁死，API 层靠这条纪律与 ADR 0002 约束。
func validateCapabilityKeysStable(existing, next []Capability) (string, bool) {
	nextByKey := make(map[string]Capability, len(next))
	for _, c := range next {
		nextByKey[c.Key] = c
	}
	for _, old := range existing {
		current, ok := nextByKey[old.Key]
		if !ok {
			// 能力项被移除：历史值仍在 capability_state / state_history 里，只是不再渲染。
			continue
		}
		if current.Type != old.Type {
			return fmt.Sprintf("能力项 %q 的类型不能修改（%s → %s）；已保存的状态值会失去解释", old.Key, old.Type, current.Type), false
		}
		if old.Type != "enum" {
			continue
		}
		values := make(map[string]bool, len(current.Options))
		for _, option := range current.Options {
			values[option.Value] = true
		}
		for _, option := range old.Options {
			if !values[option.Value] {
				return fmt.Sprintf("能力项 %q 的枚举 value %q 不能删除或改名；只能改 label 或新增选项", old.Key, option.Value), false
			}
		}
	}
	return "", true
}

// validateItemType 校验类型本身与每个能力项的定义。
// 值域规则只活在应用层，数据库里没有对应 CHECK——新增类型不该改表。
func validateItemType(in ItemType) (string, bool) {
	if !capabilityKeyPattern.MatchString(in.Key) {
		return "类型 key 必须以小写字母开头，只含小写字母、数字与下划线（最多 64 字符）", false
	}
	if in.Kind != "furniture" && in.Kind != "device" {
		return "kind 只能是 furniture 或 device", false
	}
	if strings.TrimSpace(in.Name) == "" {
		return "类型名称不能为空", false
	}
	if utf8.RuneCountInString(in.Name) > 64 {
		return "类型名称不能超过 64 个字符", false
	}
	if in.DefaultWidth < 1 || in.DefaultWidth > 5000 || in.DefaultDepth < 1 || in.DefaultDepth > 5000 {
		return "默认尺寸必须在 1–5000 厘米之间", false
	}
	if in.Kind == "furniture" && in.Driver != nil && *in.Driver != "" {
		return "只有 device 类型可以设置 driver", false
	}

	seen := make(map[string]bool, len(in.Capabilities))
	seenNames := make(map[string]bool, len(in.Capabilities))
	for i, c := range in.Capabilities {
		where := fmt.Sprintf("第 %d 个能力项", i+1)
		if !capabilityKeyPattern.MatchString(c.Key) {
			return where + "的 key 不合法（小写字母开头，只含小写字母、数字与下划线）", false
		}
		if seen[c.Key] {
			return fmt.Sprintf("能力项 key %q 重复", c.Key), false
		}
		seen[c.Key] = true
		name := strings.TrimSpace(c.Name)
		if name == "" {
			return where + "的名称不能为空", false
		}
		if seenNames[name] {
			return fmt.Sprintf("能力项名称 %q 重复", name), false
		}
		seenNames[name] = true
		if utf8.RuneCountInString(c.Name) > 64 {
			return where + "的名称不能超过 64 个字符", false
		}
		if msg, ok := validateCapabilityValueDomain(c); !ok {
			return where + "：" + msg, false
		}
	}
	return "", true
}

func validateCapabilityValueDomain(c Capability) (string, bool) {
	switch c.Type {
	case "bool":
		if c.Min != nil || c.Max != nil || c.Step != nil || len(c.Options) > 0 {
			return "bool 能力不能设置范围、步长或枚举选项", false
		}
		if c.Default != nil {
			if _, ok := c.Default.(bool); !ok {
				return "默认值必须是布尔值", false
			}
		}
	case "number":
		if len(c.Options) > 0 {
			return "number 能力不能设置枚举选项", false
		}
		if c.Min != nil && c.Max != nil && *c.Min > *c.Max {
			return "最小值不能大于最大值", false
		}
		if c.Step != nil && *c.Step <= 0 {
			return "步长必须大于 0", false
		}
		if c.Default != nil {
			value, ok := numberValue(c.Default)
			if !ok {
				return "默认值必须是数值", false
			}
			if c.Min != nil && value < *c.Min {
				return "默认值小于最小值", false
			}
			if c.Max != nil && value > *c.Max {
				return "默认值大于最大值", false
			}
			if math.IsNaN(value) || math.IsInf(value, 0) {
				return "默认值必须是有限数值", false
			}
		}
	case "enum":
		if len(c.Options) == 0 {
			return "enum 能力必须提供至少一个选项", false
		}
		values := make(map[string]bool, len(c.Options))
		for _, option := range c.Options {
			if option.Value == "" || option.Label == "" {
				return "枚举选项的 value 与 label 都不能为空", false
			}
			if values[option.Value] {
				return fmt.Sprintf("枚举选项 value %q 重复", option.Value), false
			}
			values[option.Value] = true
		}
		if c.Default != nil {
			value, ok := c.Default.(string)
			if !ok || !values[value] {
				return "枚举默认值必须是已定义的选项", false
			}
		}
		if c.Min != nil || c.Max != nil || c.Step != nil {
			return "enum 能力不能设置数值范围或步长", false
		}
	case "string":
		if c.Min != nil || c.Max != nil || c.Step != nil || len(c.Options) > 0 {
			return "string 能力不能设置范围、步长或枚举选项", false
		}
		if c.Default != nil {
			if _, ok := c.Default.(string); !ok {
				return "默认值必须是字符串", false
			}
		}
	default:
		return "type 只能是 bool / number / enum / string", false
	}
	return "", true
}

func numberValue(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
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

// checkReusedCapabilityKeys 拦住「能力项被删除后又以另一种语义加回来」。
//
// capability_state 的行不随类型元数据删除，所以一个已经退役的 key 在库里可能还留着历史值。
// 若这时把它当作全新能力项加回来，旧值会被按新类型解释（bool 的 true 变成 number 的 1），
// 前端渲染与历史曲线都会出错。历史值存在时只允许「保留 key、改 label 或关闭 writable」，
// 不允许重新定义。
func (s *Server) checkReusedCapabilityKeys(ctx context.Context, existing, next ItemType) (string, error) {
	published := make(map[string]bool, len(existing.Capabilities))
	for _, c := range existing.Capabilities {
		published[c.Key] = true
	}
	for _, c := range next.Capabilities {
		if published[c.Key] {
			continue
		}
		n, err := s.itemTypes.CountStateRowsForKey(ctx, existing.Key, c.Key)
		if err != nil {
			return "", err
		}
		if n > 0 {
			return fmt.Sprintf(
				"能力项 %q 在历史状态里还有 %d 条值，不能作为新的能力项重新加入；要继续保留请把 key 加回去并沿用原类型，或改成只读",
				c.Key, n), nil
		}
	}
	return "", nil
}
