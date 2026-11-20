#!/usr/bin/env bash
# 重新生成全部图纸（SVG）：可行性报告、需求规格说明书、ADR 共用的图。
# 依赖：plantuml 1.2026+、graphviz 12+。
# 用法：./scripts/build-diagrams.sh
# 产物提交进仓库；改图只改 src/ 下的 .puml / .dot，不要手改 svg。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC="$ROOT/docs/diagrams/src"
OUT="$ROOT/docs/diagrams/svg"

command -v plantuml >/dev/null || { echo "缺少 plantuml"; exit 1; }
command -v dot      >/dev/null || { echo "缺少 graphviz(dot)"; exit 1; }

mkdir -p "$OUT"

echo "== PlantUML =="
plantuml -tsvg -charset UTF-8 -o "$OUT" "$SRC"/*.puml

echo "== Graphviz =="
for f in "$SRC"/*.dot; do
  name="$(basename "${f%.dot}")"
  dot -Tsvg -o "$OUT/$name.svg" "$f"
  echo "  $name.svg"
done

echo "完成，输出目录：$OUT"
ls -1 "$OUT"
