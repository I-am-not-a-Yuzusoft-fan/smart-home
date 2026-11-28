package api

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api/gen"
	itemstore "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store"
)

type exportItemRecord struct {
	RowNum   int     `json:"-"`
	Name     string  `json:"name"`
	TypeKey  string  `json:"typeKey"`
	RoomName string  `json:"roomName,omitempty"`
	RoomHash string  `json:"roomHash,omitempty"`
	X        float64 `json:"x"`
	Y        float64 `json:"y"`
	Rotation float64 `json:"rotation"`
}

type exportPayload struct {
	Version    int                `json:"version"`
	ExportedAt time.Time          `json:"exportedAt"`
	Items      []exportItemRecord `json:"items"`
}

type importRequest struct {
	Format  string `json:"format"`
	Mode    string `json:"mode"`
	Content string `json:"content"`
}

type importRowError struct {
	Row     int    `json:"row"`
	Message string `json:"message"`
}

type importReport struct {
	Created int              `json:"created"`
	Updated int              `json:"updated"`
	Skipped int              `json:"skipped"`
	Errors  []importRowError `json:"errors"`
}

// ExportItems 批量导出物品（JSON 或带 UTF-8 BOM 的 CSV），仅管理员可用。
func (s *Server) ExportItems(w http.ResponseWriter, r *http.Request) {
	acct, ok := AccountFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, gen.UNAUTHENTICATED, "未登录")
		return
	}
	if acct.Role != "admin" {
		writeError(w, http.StatusForbidden, gen.FORBIDDEN, "仅管理员可导出物品")
		return
	}
	format := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("format")))
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "csv" {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "format 仅支持 json 或 csv")
		return
	}
	items, err := s.items.ListItems(r.Context(), itemstore.ItemFilter{})
	if err != nil {
		s.log.ErrorContext(r.Context(), "导出读取物品失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "导出物品失败")
		return
	}
	records := make([]exportItemRecord, 0, len(items))
	for _, it := range items {
		rec := exportItemRecord{
			Name:     it.Name,
			TypeKey:  it.TypeKey,
			X:        it.X,
			Y:        it.Y,
			Rotation: it.Rotation,
		}
		if it.RoomName != nil {
			rec.RoomName = *it.RoomName
		}
		if it.RoomHash != nil {
			rec.RoomHash = *it.RoomHash
		}
		records = append(records, rec)
	}
	dateTag := time.Now().Format("2006-01-02")
	if format == "csv" {
		var buf bytes.Buffer
		// UTF-8 BOM，避免 Excel 打开中文乱码
		buf.Write([]byte{0xEF, 0xBB, 0xBF})
		cw := csv.NewWriter(&buf)
		_ = cw.Write([]string{"name", "typeKey", "roomName", "x", "y", "rotation"})
		for _, rec := range records {
			_ = cw.Write([]string{
				rec.Name,
				rec.TypeKey,
				rec.RoomName,
				strconv.FormatFloat(rec.X, 'f', -1, 64),
				strconv.FormatFloat(rec.Y, 'f', -1, 64),
				strconv.FormatFloat(rec.Rotation, 'f', -1, 64),
			})
		}
		cw.Flush()
		w.Header().Set("Content-Type", "text/csv; charset=utf-8")
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="items-%s.csv"`, dateTag))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(buf.Bytes())
		return
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="items-%s.json"`, dateTag))
	writeJSON(w, http.StatusOK, exportPayload{
		Version:    1,
		ExportedAt: time.Now(),
		Items:      records,
	})
}

// ImportItems 批量导入物品（JSON 或 CSV），支持 skip / overwrite 冲突策略并返回逐行报告。
func (s *Server) ImportItems(w http.ResponseWriter, r *http.Request) {
	acct, ok := AccountFrom(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, gen.UNAUTHENTICATED, "未登录")
		return
	}
	if acct.Role != "admin" {
		writeError(w, http.StatusForbidden, gen.FORBIDDEN, "仅管理员可导入物品")
		return
	}
	var req importRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 2<<20))
	if err := dec.Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "请求体解析失败")
		return
	}
	format := strings.ToLower(strings.TrimSpace(req.Format))
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	if mode == "" {
		mode = "skip"
	}
	if format != "json" && format != "csv" {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "format 仅支持 json 或 csv")
		return
	}
	if mode != "skip" && mode != "overwrite" {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "mode 仅支持 skip 或 overwrite")
		return
	}

	records, parseErrs := parseImportContent(format, req.Content)
	errs := make([]importRowError, 0, len(parseErrs))
	errs = append(errs, parseErrs...)
	report := importReport{Errors: errs}
	if len(records) == 0 && len(report.Errors) > 0 {
		writeJSON(w, http.StatusOK, report)
		return
	}

	existingItems, err := s.items.ListItems(r.Context(), itemstore.ItemFilter{})
	if err != nil {
		s.log.ErrorContext(r.Context(), "导入前读取物品失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "导入失败")
		return
	}
	byName := make(map[string]itemstore.Item, len(existingItems))
	for _, it := range existingItems {
		byName[strings.TrimSpace(it.Name)] = it
	}

	roomHashByName := make(map[string]string)
	if roomNames, err := s.plan.LoadRoomNames(r.Context()); err == nil {
		for _, rm := range roomNames {
			if strings.TrimSpace(rm.Name) != "" {
				roomHashByName[strings.TrimSpace(rm.Name)] = rm.Hash
			}
		}
	}

	typeCache := make(map[string]ItemType)
	for i, rec := range records {
		rowNum := rec.RowNum
		if rowNum <= 0 {
			rowNum = i + 1
		}
		name := strings.TrimSpace(rec.Name)
		typeKey := strings.TrimSpace(rec.TypeKey)
		if name == "" {
			report.Errors = append(report.Errors, importRowError{Row: rowNum, Message: "名称不能为空"})
			continue
		}
		if typeKey == "" {
			report.Errors = append(report.Errors, importRowError{Row: rowNum, Message: "typeKey 不能为空"})
			continue
		}
		if math.IsNaN(rec.X) || math.IsInf(rec.X, 0) || math.IsNaN(rec.Y) || math.IsInf(rec.Y, 0) || math.IsNaN(rec.Rotation) || math.IsInf(rec.Rotation, 0) {
			report.Errors = append(report.Errors, importRowError{Row: rowNum, Message: "坐标或旋转角度不是合法数字"})
			continue
		}
		if rec.X < 0 || rec.X > 5000 || rec.Y < 0 || rec.Y > 5000 {
			report.Errors = append(report.Errors, importRowError{Row: rowNum, Message: "坐标须在 0–5000 cm 范围内"})
			continue
		}

		typ, cached := typeCache[typeKey]
		if !cached {
			fetched, err := s.itemTypes.GetItemType(r.Context(), typeKey)
			if err != nil {
				if errors.Is(err, ErrItemTypeNotFound) {
					report.Errors = append(report.Errors, importRowError{Row: rowNum, Message: fmt.Sprintf("类型 %q 不存在", typeKey)})
					continue
				}
				report.Errors = append(report.Errors, importRowError{Row: rowNum, Message: "查询类型失败"})
				continue
			}
			typ = fetched
			typeCache[typeKey] = fetched
		}

		var roomHash *string
		if h := strings.TrimSpace(rec.RoomHash); h != "" {
			roomHash = &h
		} else if rn := strings.TrimSpace(rec.RoomName); rn != "" {
			if h, ok := roomHashByName[rn]; ok {
				hCopy := h
				roomHash = &hCopy
			}
		}

		if existing, dup := byName[name]; dup {
			if mode == "skip" {
				report.Skipped++
				continue
			}
			upd := itemstore.ItemUpdate{
				ID:          existing.ID,
				NameSet:     true,
				Name:        name,
				PositionSet: true,
				X:           rec.X,
				Y:           rec.Y,
				Rotation:    rec.Rotation,
			}
			if roomHash != nil {
				upd.RoomSet = true
				upd.RoomHash = *roomHash
			}
			updated, err := s.items.UpdateItem(r.Context(), upd)
			if err != nil {
				report.Errors = append(report.Errors, importRowError{Row: rowNum, Message: "覆盖更新失败：" + err.Error()})
				continue
			}
			byName[name] = updated
			report.Updated++
			continue
		}

		var states []itemstore.InitialState
		if typ.Kind == "device" {
			states = initialStates(typ.Capabilities)
		}
		created, err := s.items.CreateItem(r.Context(), itemstore.ItemCreate{
			TypeKey:  typeKey,
			Name:     name,
			RoomHash: roomHash,
			X:        rec.X,
			Y:        rec.Y,
			Rotation: rec.Rotation,
			Attrs:    json.RawMessage(`{}`),
			States:   states,
		})
		if err != nil {
			report.Errors = append(report.Errors, importRowError{Row: rowNum, Message: "创建失败：" + err.Error()})
			continue
		}
		byName[name] = created
		report.Created++
	}

	writeJSON(w, http.StatusOK, report)
}

func parseImportContent(format, content string) ([]exportItemRecord, []importRowError) {
	trimmed := strings.TrimPrefix(content, "\uFEFF")
	if format == "json" {
		var wrapped exportPayload
		if err := json.Unmarshal([]byte(trimmed), &wrapped); err == nil && wrapped.Items != nil {
			return wrapped.Items, nil
		}
		var arr []exportItemRecord
		if err := json.Unmarshal([]byte(trimmed), &arr); err == nil {
			return arr, nil
		}
		return nil, []importRowError{{Row: 0, Message: "JSON 格式不合法（需为导出的对象或物品数组）"}}
	}

	cr := csv.NewReader(strings.NewReader(trimmed))
	cr.FieldsPerRecord = -1
	header, err := cr.Read()
	if err != nil {
		return nil, []importRowError{{Row: 0, Message: "CSV 为空或表头解析失败"}}
	}
	colIdx := make(map[string]int, len(header))
	for i, h := range header {
		colIdx[strings.ToLower(strings.TrimSpace(h))] = i
	}
	if _, ok := colIdx["name"]; !ok {
		return nil, []importRowError{{Row: 0, Message: "CSV 缺少必需列 name"}}
	}
	if _, ok := colIdx["typekey"]; !ok {
		return nil, []importRowError{{Row: 0, Message: "CSV 缺少必需列 typeKey"}}
	}

	var out []exportItemRecord
	var errs []importRowError
	row := 1
	for {
		fields, err := cr.Read()
		if errors.Is(err, io.EOF) {
			break
		}
		row++
		if err != nil {
			errs = append(errs, importRowError{Row: row, Message: "CSV 行解析失败"})
			continue
		}
		get := func(k string) string {
			idx, ok := colIdx[k]
			if !ok || idx >= len(fields) {
				return ""
			}
			return strings.TrimSpace(fields[idx])
		}
		xVal, errX := parseFloatOrZero(get("x"))
		yVal, errY := parseFloatOrZero(get("y"))
		rotVal, errR := parseFloatOrZero(get("rotation"))
		if errX != nil || errY != nil || errR != nil {
			errs = append(errs, importRowError{Row: row, Message: "x / y / rotation 必须为数字"})
			continue
		}
		out = append(out, exportItemRecord{
			RowNum:   row,
			Name:     get("name"),
			TypeKey:  get("typekey"),
			RoomName: get("roomname"),
			RoomHash: get("roomhash"),
			X:        xVal,
			Y:        yVal,
			Rotation: rotVal,
		})
	}
	return out, errs
}

func parseFloatOrZero(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseFloat(s, 64)
}
