package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"image"
	"io"
	"math"
	"net/http"
	"time"

	// 注册解码器：DecodeConfig 靠它们识别格式与像素尺寸。
	_ "image/jpeg"
	_ "image/png"

	_ "golang.org/x/image/webp"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api/gen"
)

const (
	// 5 MiB：一张 A3 扫描件压成 WebP 也在这个数量级，再大就是拿相机原图当底图了。
	maxUnderlayBytes = 5 << 20
	// 8000px：再大前端贴图与浏览器解码都会卡。
	maxUnderlaySide = 8000
	// 没有墙可参照时，底图按这个方形区域铺——和默认画布差不多大。
	emptyPlanWidth  = 400.0
	emptyPlanHeight = 300.0
	// 铺满时的留边比例：贴边会让墙看起来压在线框上。
	underlayFitMargin = 0.9
)

// PlanUnderlay 是底图元数据（不含字节）。
type PlanUnderlay struct {
	ContentType string
	WidthPx     int
	HeightPx    int
	Sha256      string
	CmPerPx     float64
	OffsetXCm   float64
	OffsetYCm   float64
	RotationDeg int
	Opacity     float64
	Visible     bool
	UpdatedAt   time.Time
}

// UnderlayUpload 是一次整张替换所需的全部字段。
type UnderlayUpload struct {
	ContentType string
	WidthPx     int
	HeightPx    int
	Bytes       []byte
	Sha256      string
	CmPerPx     float64
	OffsetXCm   float64
	OffsetYCm   float64
}

// UnderlayMetaPatch 是"只改出现的字段"的补丁；nil 表示不改。
type UnderlayMetaPatch struct {
	CmPerPx     *float64
	OffsetXCm   *float64
	OffsetYCm   *float64
	RotationDeg *int
	Opacity     *float64
	Visible     *bool
}

// detectUnderlay 判定图片类型与像素尺寸。
//
// 不信任请求头里的 Content-Type：浏览器对 File 对象的 type 是可伪造的，
// 而这里判定错了，后面存进去的 content_type 就会让前端用错解码器。
func detectUnderlay(data []byte) (string, int, int, error) {
	contentType := http.DetectContentType(data)
	switch contentType {
	case "image/png", "image/jpeg", "image/webp":
	default:
		return "", 0, 0, errUnderlayType
	}
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return "", 0, 0, errUnderlayDecode
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > maxUnderlaySide || cfg.Height > maxUnderlaySide {
		return "", 0, 0, errUnderlaySize
	}
	return contentType, cfg.Width, cfg.Height, nil
}

// fitUnderlay 算"铺满户型包围盒"的比例与左上角位置。
//
// 没有墙时退回默认方形区域，这样空户型也能先垫一张图照着画。
func fitUnderlay(widthPx, heightPx int, minX, minY, maxX, maxY float64) (cmPerPx, offsetX, offsetY float64) {
	w, h := maxX-minX, maxY-minY
	if w <= 0 || h <= 0 {
		minX, minY = 0, 0
		w, h = emptyPlanWidth, emptyPlanHeight
	}
	scale := math.Min(w*underlayFitMargin/float64(widthPx), h*underlayFitMargin/float64(heightPx))
	if scale <= 0 {
		scale = 1
	}
	imgW, imgH := float64(widthPx)*scale, float64(heightPx)*scale
	// 居中：包围盒中心对上图片中心。
	offsetX = minX + w/2 - imgW/2
	offsetY = minY + h/2 - imgH/2
	return scale, offsetX, offsetY
}

// planBounds 取当前墙的包围盒；没有墙时返回四个 0，由 fitUnderlay 兜底。
func (s *Server) planBounds(ctx context.Context) (minX, minY, maxX, maxY float64, err error) {
	corners, _, err := s.plan.LoadPlan(ctx)
	if err != nil {
		return 0, 0, 0, 0, err
	}
	if len(corners) == 0 {
		return 0, 0, 0, 0, nil
	}
	minX, minY = math.Inf(1), math.Inf(1)
	maxX, maxY = math.Inf(-1), math.Inf(-1)
	for _, c := range corners {
		minX = math.Min(minX, c.X)
		minY = math.Min(minY, c.Y)
		maxX = math.Max(maxX, c.X)
		maxY = math.Max(maxY, c.Y)
	}
	return minX, minY, maxX, maxY, nil
}

var (
	errUnderlayType   = underlayError("只支持 PNG / JPEG / WebP 图片")
	errUnderlayDecode = underlayError("图片内容损坏，无法读取尺寸")
	errUnderlaySize   = underlayError("图片太大：单边像素不能超过 8000")
	errUnderlayEmpty  = underlayError("没有选择图片文件")
	errUnderlayLarge  = underlayError("图片不能超过 5 MiB")
)

// ErrUnderlayMissing 表示还没有底图：PATCH 时用它区分 404 与真正的存储错误。
var ErrUnderlayMissing = errors.New("还没有底图")

func isUnderlayMissing(err error) bool { return errors.Is(err, ErrUnderlayMissing) }

type underlayError string

func (e underlayError) Error() string { return string(e) }

// UploadUnderlay 收图片原始字节，整张替换底图。
func (s *Server) UploadUnderlay(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	// 多读 1 字节用来区分"正好 5 MiB"和"超过 5 MiB"。
	data, err := io.ReadAll(io.LimitReader(r.Body, maxUnderlayBytes+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "读取图片失败")
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, errUnderlayEmpty.Error())
		return
	}
	if len(data) > maxUnderlayBytes {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, errUnderlayLarge.Error())
		return
	}
	contentType, widthPx, heightPx, err := detectUnderlay(data)
	if err != nil {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, err.Error())
		return
	}
	minX, minY, maxX, maxY, err := s.planBounds(r.Context())
	if err != nil {
		s.log.ErrorContext(r.Context(), "读取户型失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取户型失败")
		return
	}
	cmPerPx, offsetX, offsetY := fitUnderlay(widthPx, heightPx, minX, minY, maxX, maxY)
	sum := sha256.Sum256(data)
	in := UnderlayUpload{
		ContentType: contentType,
		WidthPx:     widthPx,
		HeightPx:    heightPx,
		Bytes:       data,
		Sha256:      hex.EncodeToString(sum[:]),
		CmPerPx:     cmPerPx,
		OffsetXCm:   offsetX,
		OffsetYCm:   offsetY,
	}
	if err := s.plan.SaveUnderlay(r.Context(), in); err != nil {
		s.log.ErrorContext(r.Context(), "保存底图失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "保存底图失败")
		return
	}
	s.writePlan(w, r)
}

// UpdateUnderlay 只改出现的字段：拖一次透明度不必回传整份参数。
func (s *Server) UpdateUnderlay(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	var body gen.UnderlayPatch
	if !decodeJSONBody(w, r, &body) {
		return
	}
	patch := UnderlayMetaPatch{
		CmPerPx:   body.CmPerPx,
		OffsetXCm: body.OffsetXCm,
		OffsetYCm: body.OffsetYCm,
		Opacity:   body.Opacity,
		Visible:   body.Visible,
	}
	if body.RotationDeg != nil {
		deg := int(*body.RotationDeg)
		patch.RotationDeg = &deg
	}
	if patch.CmPerPx != nil && *patch.CmPerPx <= 0 {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "比例必须大于 0")
		return
	}
	if patch.Opacity != nil && (*patch.Opacity < 0.1 || *patch.Opacity > 1) {
		writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "透明度必须在 0.1 到 1 之间")
		return
	}
	if patch.RotationDeg != nil {
		switch *patch.RotationDeg {
		case 0, 90, 180, 270:
		default:
			writeError(w, http.StatusBadRequest, gen.VALIDATIONFAILED, "旋转只能是 90 度的整数倍")
			return
		}
	}
	if _, err := s.plan.UpdateUnderlayMeta(r.Context(), patch); err != nil {
		if isUnderlayMissing(err) {
			writeError(w, http.StatusNotFound, gen.NOTFOUND, "还没有底图")
			return
		}
		s.log.ErrorContext(r.Context(), "更新底图失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "更新底图失败")
		return
	}
	s.writePlan(w, r)
}

// ClearUnderlay 删掉底图与字节；没有底图也返回 204。
func (s *Server) ClearUnderlay(w http.ResponseWriter, r *http.Request) {
	if !requireAdmin(w, r) {
		return
	}
	if _, err := s.plan.DeleteUnderlay(r.Context()); err != nil {
		s.log.ErrorContext(r.Context(), "删除底图失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "删除底图失败")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ServeUnderlayImage 按存储的 content type 原样返回图片字节。
//
// 不写进契约：一个 200 响应带三种可能的 content type 会让 codegen 生成
// 难用的 union，而这里只是一条"把字节倒出来"的路由。
func (s *Server) ServeUnderlayImage(w http.ResponseWriter, r *http.Request) {
	contentType, data, err := s.plan.LoadUnderlayImage(r.Context())
	if err != nil {
		s.log.ErrorContext(r.Context(), "读取底图失败", "err", err)
		writeError(w, http.StatusInternalServerError, gen.INTERNAL, "读取底图失败")
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusNotFound, gen.NOTFOUND, "还没有底图")
		return
	}
	w.Header().Set("Content-Type", contentType)
	// 换图会换 URL 上的 sha；同一个 sha 可以放心让浏览器缓存。
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(data); err != nil {
		s.log.WarnContext(r.Context(), "写出底图失败", "err", err)
	}
}
