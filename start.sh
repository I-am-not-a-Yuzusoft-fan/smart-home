#!/usr/bin/env bash
# 一键启动开发环境：数据库 → 后端接口 → 前端页面。
# 用法：./start.sh        停止：Ctrl-C（会一并关掉两个子进程，数据库容器保留）
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$ROOT"

API_PORT="${API_PORT:-8081}"
WEB_PORT="${WEB_PORT:-5173}"

command -v docker >/dev/null || { echo "需要 docker" >&2; exit 1; }
command -v go >/dev/null || { echo "需要 go 1.27+" >&2; exit 1; }
command -v npm >/dev/null || { echo "需要 npm" >&2; exit 1; }

echo "==> 启动数据库（postgres:17，端口 5433）"
docker compose up -d db

echo -n "==> 等待数据库就绪"
for _ in $(seq 1 60); do
  if docker exec smarthome-db pg_isready -U postgres -d smarthome >/dev/null 2>&1; then
    echo " ok"
    break
  fi
  echo -n "."
  sleep 1
done

if ! docker exec smarthome-db psql -U postgres -d smarthome -tAc \
  "SELECT 1 FROM information_schema.tables WHERE table_name = 'app_user'" | grep -q 1; then
  echo "==> 首次启动：建表并写入演示数据"
  bash scripts/reset.sh
fi

echo "==> 启动后端（:$API_PORT）"
(
  cd backend
  ADDR=":$API_PORT" go run ./cmd/api
) &
API_PID=$!

echo "==> 启动前端（:$WEB_PORT）"
(
  cd frontend
  npm run dev -- --port "$WEB_PORT"
) &
WEB_PID=$!

cleanup() {
  echo
  echo "==> 停止"
  kill "$API_PID" "$WEB_PID" 2>/dev/null || true
  wait "$API_PID" "$WEB_PID" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

echo
echo "前端： http://localhost:$WEB_PORT"
echo "接口： http://localhost:$API_PORT/api/healthz"
echo "演示账号：zhang / demo1234（管理员）、li / demo1234（家庭成员）"
echo "按 Ctrl-C 退出（数据库容器保留，可 docker compose down 清理）"

# macOS 自带 bash 3.2 没有 `wait -n`，所以自己轮询两个子进程：
# 任意一个退出就收工，避免脚本在前端崩掉后还挂在那里。
while kill -0 "$API_PID" 2>/dev/null && kill -0 "$WEB_PID" 2>/dev/null; do
  sleep 1
done
