#!/usr/bin/env python3
"""文档一致性自检 —— 四类检查，任何一条不过就退出码 1。

背景：这些错误已经真发生过三次——术语写 52 实际 66、FR 写 81 实际 62、
03 矩阵写 18 行实际 19 行。靠人眼数不准，交给脚本。

检查项：
  1. 版本一致：文件头声明的主版本，必须出现在该文件的「变更记录」里，且是最新一条
  2. 条数属实：正文里「共 N 条 FR/NFR/DR/W」的说法，必须与正则实数相等
  3. README 状态表：docs/README.md 里列的交付物文件必须存在
  4. 链接不断：docs/**/*.md 里的相对链接与图片必须能解析到真实文件

用法：
  python3 scripts/check-docs.py          # 全部检查
  python3 scripts/check-docs.py --list   # 只列出发现的问题，不解释
"""

from __future__ import annotations

import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
DOCS = ROOT / "docs"
DELIVERABLES = sorted((DOCS / "deliverables").glob("0*.md"))

OK, WARN, FAIL = "OK  ", "WARN", "FAIL"
problems: list[tuple[str, str, str]] = []  # (level, where, message)


def report(level: str, where: str, message: str) -> None:
    problems.append((level, where, message))


# ── 1. 版本一致 ────────────────────────────────────────────────
def header_version(text: str) -> str | None:
    head = "\n".join(text.splitlines()[:12])
    m = re.search(r"\*\*v(\d+\.\d+(?:\.\d+)?)", head) or re.search(r"\bv(\d+\.\d+(?:\.\d+)?)", head)
    return m.group(1) if m else None


def change_log_versions(text: str) -> tuple[list[str], bool]:
    """返回（版本列表，是否找到变更记录节）。"""
    lines = text.splitlines()
    # 取最后一个「变更记录」标题（前文可能只是提到它），标题形如
    # `## 10 变更记录` 或 `**变更记录**`
    start = None
    for i, line in enumerate(lines):
        stripped = line.lstrip()
        if "变更记录" in line and stripped.startswith(("#", "**")):
            start = i
    if start is None:
        return [], False
    # 只取标题下方那张表里的版本号：表后常有「v1.0 冻结后……」这类说明文字，
    # 若整节都扫，会把旧版本当成最新一条。
    versions: list[str] = []
    for line in lines[start + 1 :]:
        if line.startswith("## "):
            break
        if line.startswith("|"):
            versions += re.findall(r"\bv(\d+\.\d+(?:\.\d+)?)", line)
        elif versions and line.strip():
            break
    return versions, True


for path in DELIVERABLES:
    text = path.read_text(encoding="utf-8")
    where = path.relative_to(ROOT).as_posix()
    hv = header_version(text)
    if hv is None:
        report(FAIL, where, "文件头找不到版本号（形如 **v1.2**）")
        continue
    versions, found = change_log_versions(text)
    if not found:
        report(WARN, where, f"没有「变更记录」节（文件头声明 v{hv}）")
    elif hv not in versions:
        report(FAIL, where, f"文件头写 v{hv}，但变更记录里最新是 v{versions[-1] if versions else '无'}")
    elif versions[-1] != hv:
        report(FAIL, where, f"文件头 v{hv} 与变更记录最后一条 v{versions[-1]} 不一致")


# ── 2. 条数属实 ────────────────────────────────────────────────
ID_PATTERNS = {
    "FR": r"\bFR-\d+\.\d+\b",
    "NFR": r"\bNFR-\d+\b",
    "DR": r"\bDR-\d+\b",
    "W": r"\bW\d+\b",
}

# 条数的**事实来源**是需求规格说明书本身：别的文档说「共 62 条 FR」，
# 要拿 SRS 里真实的编号数量去对，而不是数它自己引用了多少条。
SRS = DOCS / "deliverables" / "02-需求规格说明书.md"
srs_text = SRS.read_text(encoding="utf-8") if SRS.exists() else ""
actual_counts = {
    name: len(set(re.findall(pattern, srs_text))) for name, pattern in ID_PATTERNS.items()
}

for path in DELIVERABLES:
    text = path.read_text(encoding="utf-8")
    where = path.relative_to(ROOT).as_posix()
    for name, actual in actual_counts.items():
        # 正文里「…共 N 条 FR…」这类声明
        for m in re.finditer(r"共\s*(\d+)\s*条", text):
            window = text[max(0, m.start() - 30) : m.start()]
            if name not in window:
                continue
            claim = int(m.group(1))
            if claim != actual:
                report(FAIL, where, f"声称共 {claim} 条 {name}，SRS 实际 {actual} 条")


# ── 3. README 状态表里的文件必须存在 ──────────────────────────
readme = DOCS / "README.md"
if readme.exists():
    text = readme.read_text(encoding="utf-8")
    for target in re.findall(r"\]\((deliverables/[^)]+\.md)\)", text):
        if not (DOCS / target).exists():
            report(FAIL, "docs/README.md", f"状态表引用的文件不存在：{target}")


# ── 4. 相对链接与图片必须能解析 ───────────────────────────────
LINK = re.compile(r"!?\[[^\]]*\]\(([^)\s]+)(?:\s+\"[^\"]*\")?\)")

for md in sorted(DOCS.rglob("*.md")):
    where = md.relative_to(ROOT).as_posix()
    for raw in LINK.findall(md.read_text(encoding="utf-8")):
        if raw.startswith(("http://", "https://", "mailto:", "#")):
            continue
        target = raw.split("#", 1)[0].strip("<>")
        if not target:
            continue
        if not (md.parent / target).exists():
            report(FAIL, where, f"链接目标不存在：{target}")


# ── 输出 ───────────────────────────────────────────────────────
if "--list" in sys.argv:
    for level, where, message in problems:
        print(f"{level} {where}: {message}")
else:
    counts = {OK: 0, WARN: 0, FAIL: 0}
    for level, where, message in problems:
        counts[level] += 1
        print(f"{level} {where}\n       {message}")
    if not problems:
        print("OK   四类检查全部通过：版本一致、条数属实、状态表文件存在、链接无断链")
    else:
        print(
            f"\n汇总：FAIL {counts[FAIL]} · WARN {counts[WARN]}"
            + ("" if counts[FAIL] else "（无阻断项）")
        )

sys.exit(1 if any(level == FAIL for level, _, _ in problems) else 0)
