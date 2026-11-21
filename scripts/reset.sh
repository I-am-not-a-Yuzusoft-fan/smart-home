#!/usr/bin/env bash
# 一次性重建演示数据：drop schema → 建表 → seed
# 用途：演示前恢复到已知状态；也用来验证「十秒重建」这条验收标准。
# 前提：数据库可达（docker compose up -d db 之后）。
set -euo pipefail

DB_URL="${DB_URL:-postgres://postgres:spike@127.0.0.1:5433/smarthome?sslmode=disable}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# 两条通路：装了 psql 就直接连；没装（Windows 队友常见）就走容器内的 psql。
# 走容器时忽略 DB_URL —— 目标就是 compose 起的那只库。
if command -v psql >/dev/null 2>&1; then
  psql_cmd() { psql "$DB_URL" -q -v ON_ERROR_STOP=1 -c "$1"; }
  psql_file() { psql "$DB_URL" -q -v ON_ERROR_STOP=1 -f "$1"; }
  psql_scalar() { psql "$DB_URL" -tA -c "$1"; }
else
  command -v docker >/dev/null 2>&1 || {
    echo "既没有 psql，也没有 docker：装一个，或用 docker compose up -d db 后重试" >&2; exit 1; }
  psql_cmd() { docker compose exec -T db psql -U postgres -d smarthome -q -v ON_ERROR_STOP=1 -c "$1"; }
  psql_file() { docker compose exec -T db psql -U postgres -d smarthome -q -v ON_ERROR_STOP=1 < "$1"; }
  psql_scalar() { docker compose exec -T db psql -U postgres -d smarthome -tA -c "$1"; }
  echo "（未发现 psql 客户端，改用容器内的 psql）"
fi

start=$(date +%s)
psql_cmd 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;'
psql_file "$ROOT/sql/0001_init.sql"
psql_file "$ROOT/sql/seed.sql"
end=$(date +%s)

psql_scalar "SELECT 'items=' || count(*) FROM item;"
echo "重建完成：$((end - start)) 秒"
