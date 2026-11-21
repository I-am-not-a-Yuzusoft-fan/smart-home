package api_test

import (
	"testing"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"

	"github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/api"
)

func testUUID() openapi_types.UUID {
	var u openapi_types.UUID
	for i := range u {
		u[i] = byte(i + 1)
	}
	return u
}

func TestSessionRoundTrip(t *testing.T) {
	c := api.NewSessionCodec("s3cret", time.Hour)
	uid := testUUID()
	got, err := c.Verify(c.Sign(uid))
	if err != nil {
		t.Fatalf("合法会话不应报错: %v", err)
	}
	if got.String() != uid.String() {
		t.Fatalf("用户 id 往返不一致：期望 %s，实际 %s", uid, got)
	}
}

func TestSessionRejectsTampering(t *testing.T) {
	c := api.NewSessionCodec("s3cret", time.Hour)
	signed := c.Sign(testUUID())
	if _, err := c.Verify(signed + "x"); err == nil {
		t.Fatal("篡改签名后必须校验失败")
	}
	other := api.NewSessionCodec("another-secret", time.Hour)
	if _, err := other.Verify(signed); err == nil {
		t.Fatal("换了密钥后必须校验失败")
	}
}

func TestSessionRejectsExpired(t *testing.T) {
	c := api.NewSessionCodec("s3cret", -time.Second)
	if _, err := c.Verify(c.Sign(testUUID())); err == nil {
		t.Fatal("过期会话必须校验失败")
	}
}

func TestSessionRejectsMalformed(t *testing.T) {
	c := api.NewSessionCodec("s3cret", time.Hour)
	for _, v := range []string{"", "a", "a.b", "a.b.c.d", "not-a-uuid.9999999999.sig"} {
		if _, err := c.Verify(v); err == nil {
			t.Fatalf("畸形会话值 %q 必须校验失败", v)
		}
	}
}
