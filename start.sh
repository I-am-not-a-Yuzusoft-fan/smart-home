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
command -v node >/dev/null || { echo "需要 Node 20+" >&2; exit 1; }
# psql 不是硬依赖：没有它会自动走容器内的 psql（见 scripts/reset.sh）

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

# 先装前端依赖：node_modules 不入库，干净克隆上没有它
if [ ! -d frontend/node_modules ]; then
  echo "==> 安装前端依赖（首次启动需要几分钟）"
  ( cd frontend && npm ci )
fi

# 会话密钥每次启动随机生成：默认常量写在源码里，等于谁都能伪造 Cookie
export SESSION_SECRET="${SESSION_SECRET:-dev-$(date +%s)-$RANDOM}"

echo "==> 编译并启动后端（:${API_PORT}）"
# 直接跑编译产物而不是 go run：go run 不转发信号，Ctrl-C 后子进程会留着占端口
# 产物落仓库内 .cache/：Windows 上 Go 会补 .exe，写死 /tmp 的名字会找不到文件
mkdir -p "$ROOT/.cache"
API_BIN="$ROOT/.cache/smarthome-api"
case "$(uname -s)" in MINGW*|MSYS*|CYGWIN*) API_BIN="$API_BIN.exe" ;; esac
( cd backend && go build -o "$API_BIN" ./cmd/api )
ADDR=":$API_PORT" "$API_BIN" &
API_PID=$!

echo "==> 启动前端（:${WEB_PORT}）"
(
  cd frontend
  # 用 node 直接跑入口而不是 .bin 里的包装脚本：Windows 上那是 .cmd，bash 调不动
  API_PORT="$API_PORT" node node_modules/vite/bin/vite.js --port "$WEB_PORT" --strictPort
) &
WEB_PID=$!

STOPPED=0
cleanup() {
  # INT/TERM 与 EXIT 都会触发，只做一次
  [ "$STOPPED" = "1" ] && return
  STOPPED=1
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
