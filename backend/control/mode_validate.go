package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/santhosh-tekuri/jsonschema/v6"

	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

var (
	ErrModeValidation   = errors.New("场景定义校验失败")
	ErrModeNameConflict = errors.New("同名场景已存在")
)

// modeDefinitionSchemaJSON 与 openapi.yaml 中 ModeDefinition 同源的 JSON Schema 2020-12 约束。
const modeDefinitionSchemaJSON = `{
  "$schema": "https://json-schema.org/draft/2020-12/schema",
  "type": "object",
  "required": ["version", "steps"],
  "additionalProperties": false,
  "properties": {
    "version": { "type": "integer", "minimum": 1 },
    "guard": { "$ref": "#/$defs/guard" },
    "defaults": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "continueOnError": { "type": "boolean" }
      }
    },
    "steps": {
      "type": "array",
      "minItems": 1,
      "items": { "$ref": "#/$defs/node" }
    }
  },
  "$defs": {
    "condition": {
      "type": "object",
      "required": ["itemId", "capability", "op", "value"],
      "additionalProperties": false,
      "properties": {
        "itemId": { "type": "string", "minLength": 1 },
        "capability": { "type": "string", "minLength": 1 },
        "op": { "type": "string", "enum": ["==", "!=", ">", ">=", "<", "<="] },
        "value": {}
      }
    },
    "guard": {
      "type": "object",
      "additionalProperties": false,
      "properties": {
        "all": { "type": "array", "items": { "$ref": "#/$defs/condition" } },
        "any": { "type": "array", "items": { "$ref": "#/$defs/condition" } }
      }
    },
    "node": {
      "type": "object",
      "required": ["nodeId", "type"],
      "additionalProperties": false,
      "properties": {
        "nodeId": { "type": "string", "minLength": 1, "maxLength": 64 },
        "type": { "type": "string", "enum": ["action", "delay", "choose"] },
        "alias": { "type": "string", "maxLength": 128 },
        "enabled": { "type": "boolean" },
        "continueOnError": { "type": "boolean" },
        "itemId": { "type": "string", "minLength": 1 },
        "capability": { "type": "string", "minLength": 1 },
        "value": {},
        "valueFrom": { "type": "string", "enum": ["comfort"] },
        "ms": { "type": "integer", "minimum": 0, "maximum": 300000 },
        "when": { "$ref": "#/$defs/guard" },
        "then": { "type": "array", "items": { "$ref": "#/$defs/node" } },
        "else": { "type": "array", "items": { "$ref": "#/$defs/node" } }
      }
    }
  }
}`

var (
	modeSchemaOnce sync.Once
	modeSchema     *jsonschema.Schema
	modeSchemaErr  error
)

func compiledModeSchema() (*jsonschema.Schema, error) {
	modeSchemaOnce.Do(func() {
		doc, err := jsonschema.UnmarshalJSON(bytes.NewReader([]byte(modeDefinitionSchemaJSON)))
		if err != nil {
			modeSchemaErr = err
			return
		}
		c := jsonschema.NewCompiler()
		const schemaURL = "https://smarthome.local/mode-definition.schema.json"
		if err := c.AddResource(schemaURL, doc); err != nil {
			modeSchemaErr = err
			return
		}
		modeSchema, modeSchemaErr = c.Compile(schemaURL)
	})
	return modeSchema, modeSchemaErr
}

// ValidateModeDefinition 先用 JSON Schema 2020-12 校验结构，再校验 nodeId 唯一性、节点语义与引用物品存在性。
func ValidateModeDefinition(ctx context.Context, pool *pgxpool.Pool, raw json.RawMessage) error {
	schema, err := compiledModeSchema()
	if err != nil {
		return fmt.Errorf("编译场景 Schema 失败: %w", err)
	}
	inst, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return fmt.Errorf("%w: definition 不是合法 JSON", ErrModeValidation)
	}
	if err := schema.Validate(inst); err != nil {
		return fmt.Errorf("%w: %v", ErrModeValidation, err)
	}

	var def ModeDefinition
	if err := json.Unmarshal(raw, &def); err != nil {
		return fmt.Errorf("%w: 解析 definition 失败", ErrModeValidation)
	}

	q := storegen.New(pool)
	checkItemExists := func(id uuid.UUID, label string) error {
		if id == uuid.Nil {
			return fmt.Errorf("%w: %s 缺少有效设备 ID", ErrModeValidation, label)
		}
		if _, err := q.GetItem(ctx, pgUUID(id)); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return fmt.Errorf("%w: %s 引用的设备不存在", ErrModeValidation, label)
			}
			return err
		}
		return nil
	}

	checkGuard := func(g *ModeGuard, label string) error {
		if g == nil {
			return nil
		}
		for _, c := range g.All {
			if err := checkItemExists(c.ItemID, label+"(all)"); err != nil {
				return err
			}
		}
		for _, c := range g.Any {
			if err := checkItemExists(c.ItemID, label+"(any)"); err != nil {
				return err
			}
		}
		return nil
	}

	if err := checkGuard(def.Guard, "顶层守卫"); err != nil {
		return err
	}

	seenNodes := make(map[string]bool)
	var walk func(nodes []ModeNode) error
	walk = func(nodes []ModeNode) error {
		for _, n := range nodes {
			if strings.TrimSpace(n.NodeID) == "" || n.NodeID == GuardNodeID {
				return fmt.Errorf("%w: 非法 nodeId %q", ErrModeValidation, n.NodeID)
			}
			if seenNodes[n.NodeID] {
				return fmt.Errorf("%w: 存在重复 nodeId %q", ErrModeValidation, n.NodeID)
			}
			seenNodes[n.NodeID] = true

			switch n.Type {
			case "action":
				if n.ItemID == nil {
					return fmt.Errorf("%w: 动作节点 %s 缺少 itemId", ErrModeValidation, n.NodeID)
				}
				if err := checkItemExists(*n.ItemID, "动作节点 "+n.NodeID); err != nil {
					return err
				}
				if n.ValueFrom != "comfort" {
					if strings.TrimSpace(n.Capability) == "" {
						return fmt.Errorf("%w: 动作节点 %s 缺少 capability", ErrModeValidation, n.NodeID)
					}
					if n.Value == nil {
						return fmt.Errorf("%w: 动作节点 %s 缺少 value", ErrModeValidation, n.NodeID)
					}
				}
			case "delay":
				if n.Ms < 0 || n.Ms > 300000 {
					return fmt.Errorf("%w: 延时节点 %s 的 ms 必须在 0–300000 之间", ErrModeValidation, n.NodeID)
				}
			case "choose":
				if n.When == nil || (len(n.When.All) == 0 && len(n.When.Any) == 0) {
					return fmt.Errorf("%w: 条件分支 %s 必须至少包含一条 when 条件", ErrModeValidation, n.NodeID)
				}
				if err := checkGuard(n.When, "条件分支 "+n.NodeID); err != nil {
					return err
				}
				if err := walk(n.Then); err != nil {
					return err
				}
				if err := walk(n.Else); err != nil {
					return err
				}
			}
		}
		return nil
	}
	return walk(def.Steps)
}
