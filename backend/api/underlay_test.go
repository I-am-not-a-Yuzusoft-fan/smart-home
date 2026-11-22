package api_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"math"
	"net/http"
	"net/http/httptest"
	"testing"
)

// doBytes 发原始请求体：底图上传走 application/octet-stream，不是 JSON。
func doBytes(t *testing.T, h http.Handler, method, path string, body []byte, cookie *http.Cookie) *httptest.ResponseRecorder {
	t.Helper()
	r := httptest.NewRequest(method, path, bytes.NewReader(body))
	r.Header.Set("Content-Type", "application/octet-stream")
	if method != http.MethodGet && method != http.MethodHead {
		r.Header.Set("X-Requested-With", "fetch")
	}
	if cookie != nil {
		r.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

// pngBytes 生成一张缩小到几 KB 的纯色 PNG，供上传用例使用。
func pngBytes(t *testing.T, width, height int) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("生成 PNG 失败: %v", err)
	}
	return buf.Bytes()
}

type underlayBody struct {
	ContentType string  `json:"contentType"`
	WidthPx     int     `json:"widthPx"`
	HeightPx    int     `json:"heightPx"`
	Sha256      string  `json:"sha256"`
	CmPerPx     float64 `json:"cmPerPx"`
	OffsetXCm   float64 `json:"offsetXCm"`
	OffsetYCm   float64 `json:"offsetYCm"`
	RotationDeg int     `json:"rotationDeg"`
	Opacity     float64 `json:"opacity"`
	Visible     bool    `json:"visible"`
}

type planWithUnderlay struct {
	Underlay *underlayBody `json:"underlay"`
}

func readUnderlay(t *testing.T, h http.Handler, cookie *http.Cookie) *underlayBody {
	t.Helper()
	w := do(t, h, http.MethodGet, "/api/plan", "", cookie)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/plan 应 200，实际 %d：%s", w.Code, w.Body.String())
	}
	var p planWithUnderlay
	if err := json.Unmarshal(w.Body.Bytes(), &p); err != nil {
		t.Fatalf("解析户型失败: %v", err)
	}
	return p.Underlay
}

func uploadUnderlay(t *testing.T, h http.Handler, cookie *http.Cookie, data []byte) *httptest.ResponseRecorder {
	t.Helper()
	return doBytes(t, h, http.MethodPut, "/api/plan/underlay", data, cookie)
}

func TestUploadUnderlayStoresMetadataAndServesImage(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")
	t.Cleanup(func() { do(t, h, http.MethodDelete, "/api/plan/underlay", "", admin) })

	data := pngBytes(t, 240, 120)
	w := uploadUnderlay(t, h, admin, data)
	if w.Code != http.StatusOK {
		t.Fatalf("上传应 200，实际 %d：%s", w.Code, w.Body.String())
	}
	u := readUnderlay(t, h, admin)
	if u == nil {
		t.Fatal("上传后户型里应有底图")
	}
	if u.ContentType != "image/png" || u.WidthPx != 240 || u.HeightPx != 120 {
		t.Fatalf("底图类型或尺寸不对：%+v", u)
	}
	if u.Sha256 == "" {
		t.Fatal("应记录图片摘要")
	}
	if u.RotationDeg != 0 || u.Opacity != 0.45 || !u.Visible {
		t.Fatalf("换图应重置显示参数，实际 %+v", u)
	}

	// 取回的字节要原样一致：前端拿它当图源，差一个字节都是坏图。
	got := do(t, h, http.MethodGet, "/api/plan/underlay/image", "", admin)
	if got.Code != http.StatusOK {
		t.Fatalf("取图应 200，实际 %d", got.Code)
	}
	if got.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("取图 content type 应为 image/png，实际 %q", got.Header().Get("Content-Type"))
	}
	if !bytes.Equal(got.Body.Bytes(), data) {
		t.Fatalf("取回的图片字节与上传的不一致：%d vs %d 字节", got.Body.Len(), len(data))
	}
}

func TestUploadUnderlayRejectsNonImage(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")
	w := uploadUnderlay(t, h, admin, []byte("this is not an image at all"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("非图片应 400，实际 %d：%s", w.Code, w.Body.String())
	}
}

func TestUploadUnderlayRejectsOversizedBody(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")
	w := uploadUnderlay(t, h, admin, bytes.Repeat([]byte{0}, 5<<20+1))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("超过 5 MiB 应 400，实际 %d：%s", w.Code, w.Body.String())
	}
}

func TestUpdateUnderlayMetaMergesAndValidates(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")
	t.Cleanup(func() { do(t, h, http.MethodDelete, "/api/plan/underlay", "", admin) })
	if w := uploadUnderlay(t, h, admin, pngBytes(t, 64, 32)); w.Code != http.StatusOK {
		t.Fatalf("准备底图失败：%d %s", w.Code, w.Body.String())
	}

	// 只传透明度：比例与偏移保持不变。
	w := do(t, h, http.MethodPatch, "/api/plan/underlay", `{"opacity":0.8,"visible":false}`, admin)
	if w.Code != http.StatusOK {
		t.Fatalf("改元数据应 200，实际 %d：%s", w.Code, w.Body.String())
	}
	u := readUnderlay(t, h, admin)
	if u == nil || math.Abs(u.Opacity-0.8) > 1e-9 || u.Visible {
		t.Fatalf("透明度/显隐没有写进去：%+v", u)
	}
	if u.CmPerPx <= 0 {
		t.Fatalf("只改透明度不应动比例，实际 %v", u.CmPerPx)
	}

	bad := []string{
		`{"opacity":2}`,
		`{"rotationDeg":45}`,
		`{"cmPerPx":0}`,
	}
	for _, body := range bad {
		w := do(t, h, http.MethodPatch, "/api/plan/underlay", body, admin)
		if w.Code != http.StatusBadRequest {
			t.Fatalf("%s 应 400，实际 %d", body, w.Code)
		}
	}
}

func TestUpdateUnderlayMetaWithoutUnderlayIs404(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")
	do(t, h, http.MethodDelete, "/api/plan/underlay", "", admin)
	w := do(t, h, http.MethodPatch, "/api/plan/underlay", `{"opacity":0.8}`, admin)
	if w.Code != http.StatusNotFound {
		t.Fatalf("没有底图时改元数据应 404，实际 %d：%s", w.Code, w.Body.String())
	}
}

func TestUnderlayRequiresAdmin(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")
	member := loginAs(t, h, "li")
	t.Cleanup(func() { do(t, h, http.MethodDelete, "/api/plan/underlay", "", admin) })

	if w := uploadUnderlay(t, h, member, pngBytes(t, 32, 32)); w.Code != http.StatusForbidden {
		t.Fatalf("成员上传应 403，实际 %d", w.Code)
	}
	if w := uploadUnderlay(t, h, admin, pngBytes(t, 32, 32)); w.Code != http.StatusOK {
		t.Fatalf("管理员上传应 200，实际 %d：%s", w.Code, w.Body.String())
	}
	if w := do(t, h, http.MethodPatch, "/api/plan/underlay", `{"opacity":0.5}`, member); w.Code != http.StatusForbidden {
		t.Fatalf("成员改底图应 403，实际 %d", w.Code)
	}
	if w := do(t, h, http.MethodDelete, "/api/plan/underlay", "", member); w.Code != http.StatusForbidden {
		t.Fatalf("成员删底图应 403，实际 %d", w.Code)
	}
}

func TestDeleteUnderlayIsIdempotent(t *testing.T) {
	h := newTestServer(t)
	admin := loginAs(t, h, "zhang")
	if w := uploadUnderlay(t, h, admin, pngBytes(t, 32, 32)); w.Code != http.StatusOK {
		t.Fatalf("准备底图失败：%d", w.Code)
	}
	if w := do(t, h, http.MethodDelete, "/api/plan/underlay", "", admin); w.Code != http.StatusNoContent {
		t.Fatalf("删除应 204，实际 %d", w.Code)
	}
	if u := readUnderlay(t, h, admin); u != nil {
		t.Fatalf("删除后不应再有底图，实际 %+v", u)
	}
	if w := do(t, h, http.MethodDelete, "/api/plan/underlay", "", admin); w.Code != http.StatusNoContent {
		t.Fatalf("重复删除也应 204，实际 %d", w.Code)
	}
	if w := do(t, h, http.MethodGet, "/api/plan/underlay/image", "", admin); w.Code != http.StatusNotFound {
		t.Fatalf("没有底图时取图应 404，实际 %d", w.Code)
	}
}
