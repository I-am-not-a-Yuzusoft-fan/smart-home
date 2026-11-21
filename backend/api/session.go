// Package api 实现 HTTP 接口层：鉴权、会话、错误封套，以及与契约对应的 handler。
package api

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	openapi_types "github.com/oapi-codegen/runtime/types"
)

// ErrInvalidSession 表示会话 Cookie 缺失、签名不符或已过期。
var ErrInvalidSession = errors.New("会话无效")

// SessionCodec 把「用户 id + 过期时间」签成不透明 Cookie 值。
//
// 不用服务端会话表：Cookie 自带过期时间，签名保证不可伪造；
// 退出登录只需让浏览器丢弃它，代价是无法逐个吊销——MVP 阶段够用。
type SessionCodec struct {
	secret []byte
	ttl    time.Duration
	now    func() time.Time
}

// NewSessionCodec 用固定密钥与有效时长构造编解码器。
func NewSessionCodec(secret string, ttl time.Duration) *SessionCodec {
	return &SessionCodec{secret: []byte(secret), ttl: ttl, now: time.Now}
}

// TTL 返回会话有效期，供 Cookie 的 MaxAge 使用。
func (c *SessionCodec) TTL() time.Duration { return c.ttl }

// Sign 生成 "<uid>.<exp>.<sig>" 形式的会话值。
func (c *SessionCodec) Sign(uid openapi_types.UUID) string {
	payload := fmt.Sprintf("%s.%d", uid.String(), c.now().Add(c.ttl).Unix())
	return payload + "." + c.mac(payload)
}

// Verify 校验签名与过期时间，返回其中的用户 id。
func (c *SessionCodec) Verify(value string) (openapi_types.UUID, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 3 {
		return openapi_types.UUID{}, ErrInvalidSession
	}
	payload := parts[0] + "." + parts[1]
	if !hmac.Equal([]byte(c.mac(payload)), []byte(parts[2])) {
		return openapi_types.UUID{}, ErrInvalidSession
	}
	exp, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || c.now().Unix() >= exp {
		return openapi_types.UUID{}, ErrInvalidSession
	}
	var uid openapi_types.UUID
	if err := uid.UnmarshalText([]byte(parts[0])); err != nil {
		return openapi_types.UUID{}, ErrInvalidSession
	}
	return uid, nil
}

func (c *SessionCodec) mac(payload string) string {
	h := hmac.New(sha256.New, c.secret)
	h.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}
