// Package store_test 验证生成的查询对真实 PostgreSQL 的行为。
//
// 这是 spec 里约定的 store 接缝：对真库跑，不打桩——
// 生成代码的坑（jsonb、uuid、可空列）只有真库能暴露。
package store_test

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	storegen "github.com/I-am-not-a-Yuzusoft-fan/smart-home/backend/store/gen"
)

func newQueries(t *testing.T) *storegen.Queries {
	t.Helper()
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("未设置 TEST_DATABASE_URL，跳过需要数据库的测试")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("连接数据库失败: %v", err)
	}
	t.Cleanup(pool.Close)
	return storegen.New(pool)
}

func TestGetUserByUsername(t *testing.T) {
	q := newQueries(t)
	row, err := q.GetUserByUsername(context.Background(), "zhang")
	if err != nil {
		t.Fatalf("查询 zhang 失败: %v", err)
	}
	if row.Role != "admin" {
		t.Errorf("zhang 的角色应为 admin，实际 %q", row.Role)
	}
	if row.PasswordHash == "" {
		t.Error("口令哈希不应为空")
	}
}

func TestGetUserByIDMatchesUsernameLookup(t *testing.T) {
	q := newQueries(t)
	byName, err := q.GetUserByUsername(context.Background(), "li")
	if err != nil {
		t.Fatalf("查询 li 失败: %v", err)
	}
	byID, err := q.GetUserByID(context.Background(), byName.ID)
	if err != nil {
		t.Fatalf("按 id 查询失败: %v", err)
	}
	if byID.Username != byName.Username {
		t.Fatalf("两次查询用户名不一致：%s vs %s", byID.Username, byName.Username)
	}
}

func TestUnknownUserReportsNoRows(t *testing.T) {
	q := newQueries(t)
	if _, err := q.GetUserByUsername(context.Background(), "nobody-here"); err == nil {
		t.Fatal("不存在的账号应返回错误")
	}
}
