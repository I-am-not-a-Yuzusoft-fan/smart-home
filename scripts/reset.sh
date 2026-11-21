#!/usr/bin/env bash
# 一次性重建演示数据：drop schema → 建表 → seed
# 用途：演示前恢复到已知状态；也用来验证「十秒重建」这条验收标准。
# 前提：数据库可达（docker compose up -d db 之后）。
set -euo pipefail

DB_URL="${DB_URL:-postgres://postgres:spike@127.0.0.1:5433/smarthome?sslmode=disable}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

start=$(date +%s)
psql "$DB_URL" -q -v ON_ERROR_STOP=1 -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public;'
psql "$DB_URL" -q -v ON_ERROR_STOP=1 -f "$ROOT/sql/0001_init.sql"
psql "$DB_URL" -q -v ON_ERROR_STOP=1 -f "$ROOT/sql/seed.sql"
end=$(date +%s)

psql "$DB_URL" -tAc "SELECT 'items=' || count(*) FROM item;"
echo "重建完成：$((end - start)) 秒"
