#!/usr/bin/env python3
"""生成现代 CAD 风格的户型图（底图功能的示例素材 + 定标测试基准）。

每张图的比例精确已知（manifest 里给出 cm_per_px 与两个定标点），
用于验证「两点定标」的数学是否正确。

用法：
    python3 scripts/gen_underlay_samples.py            # 输出到 frontend/public/underlays/samples/synthetic/
"""
from __future__ import annotations

import json
import math
import os
from dataclasses import dataclass, field
from pathlib import Path

from PIL import Image, ImageDraw, ImageFont

SS = 3  # 超采样倍数，画完再缩回去，线条才有抗锯齿
TARGET_W = 1600  # 输出宽度（像素）
MARGIN_CM = 110  # 图外留给尺寸线的边距

FONT_DIR = "/System/Library/Fonts/Supplemental"
CJK_REG = "/System/Library/Fonts/Hiragino Sans GB.ttc"
CJK_BOLD = "/System/Library/Fonts/STHeiti Medium.ttc"
LATIN_REG = f"{FONT_DIR}/Arial.ttf"
LATIN_BOLD = f"{FONT_DIR}/Arial Bold.ttf"

WALL = (58, 58, 58)
WALL_LIGHT = (120, 120, 120)
INK = (45, 45, 45)
DIM = (128, 128, 128)
PAPER = (253, 252, 250)

FLOOR = {
    "living": (253, 240, 224),
    "bedroom": (232, 238, 250),
    "kitchen": (233, 245, 234),
    "bath": (228, 243, 247),
    "hall": (243, 242, 245),
    "office": (238, 243, 238),
    "study": (243, 238, 248),
    "balcony": (239, 246, 240),
}

FURN = (255, 255, 255)
FURN_LINE = (140, 140, 140)
FURN_SOFT = (238, 238, 238)


@dataclass
class Room:
    name: str
    kind: str
    x: float
    y: float
    w: float
    h: float
    label_dx: float = 0.0
    label_dy: float = 0.0
    poly: list[tuple[float, float]] | None = None  # 非矩形房间（L 形、斜墙）

    @property
    def area(self) -> float:
        return round(self.w * self.h / 10000, 1)


@dataclass
class Opening:
    wall: int  # 第几面墙
    offset: float  # 从墙起点算起的偏移（cm）
    width: float
    kind: str = "door"  # door | window | pass
    side: int = 1  # 门扇朝法线正方向还是负方向


@dataclass
class Panel:
    walls: list[tuple[tuple[float, float], tuple[float, float], float]] = field(default_factory=list)
    rooms: list[Room] = field(default_factory=list)
    openings: list[Opening] = field(default_factory=list)
    furniture: list[tuple] = field(default_factory=list)
    dims_x: list[float] = field(default_factory=list)
    dims_y: list[float] = field(default_factory=list)
    dx: float = 0.0
    dy: float = 0.0
    caption: str = ""
    dim_sides: tuple[str, ...] = ("top", "bottom", "left", "right")


@dataclass
class Plan:
    name: str
    title: str
    panels: list[Panel]
    w: float
    h: float


def load_font(size_px: int, bold: bool = False):
    for path in ((CJK_BOLD if bold else CJK_REG), (LATIN_BOLD if bold else LATIN_REG)):
        try:
            return ImageFont.truetype(path, size_px)
        except OSError:
            continue
    return ImageFont.load_default()


class Surface:
    """把厘米坐标换算成像素并按超采样倍数放大的画布。"""

    def __init__(self, w_cm: float, h_cm: float, scale: float):
        self.scale = scale  # px per cm（最终图）
        self.w = int(round(w_cm * scale))
        self.h = int(round(h_cm * scale))
        self.img = Image.new("RGB", (self.w * SS, self.h * SS), PAPER)
        self.d = ImageDraw.Draw(self.img, "RGBA")
        self.ox = self.oy = MARGIN_CM  # 把户型整体推到留白里，四周才放得下尺寸线

    def px(self, cm: float) -> float:
        return cm * self.scale * SS

    def pt(self, x: float, y: float) -> tuple[float, float]:
        return (self.px(x + self.ox), self.px(y + self.oy))

    def line(self, pts, width_cm: float, fill=WALL, cap="round"):
        d = self.d
        w = max(1, int(round(self.px(width_cm))))
        d.line([self.pt(x, y) for x, y in pts], fill=fill, width=w, joint="curve")
        if cap == "round":
            for x, y in (pts[0], pts[-1]):
                r = w / 2
                cx, cy = self.pt(x, y)
                d.ellipse([cx - r, cy - r, cx + r, cy + r], fill=fill)

    def rect(self, x, y, w, h, fill=None, outline=None, width_cm=1.2, radius_cm=0.0):
        box = [self.px(x + self.ox), self.px(y + self.oy),
               self.px(x + w + self.ox), self.px(y + h + self.oy)]
        ow = max(1, int(round(self.px(width_cm))))
        if radius_cm:
            self.d.rounded_rectangle(box, radius=self.px(radius_cm), fill=fill, outline=outline, width=ow)
        else:
            self.d.rectangle(box, fill=fill, outline=outline, width=ow)

    def poly(self, pts, fill=None, outline=None, width_cm=1.2):
        p = [self.pt(x, y) for x, y in pts]
        if fill is not None:
            self.d.polygon(p, fill=fill)
        if outline is not None:
            self.d.line(p + [p[0]], fill=outline, width=max(1, int(round(self.px(width_cm)))), joint="curve")

    def circle(self, cx, cy, r, fill=None, outline=None, width_cm=1.2):
        box = [self.px(cx - r + self.ox), self.px(cy - r + self.oy),
               self.px(cx + r + self.ox), self.px(cy + r + self.oy)]
        self.d.ellipse(box, fill=fill, outline=outline, width=max(1, int(round(self.px(width_cm)))))

    def text(self, x, y, s, size_px, fill=INK, anchor="mm", bold=False, angle=0):
        font = load_font(int(size_px * SS), bold)
        if angle:
            tmp = Image.new("RGBA", (int(2000 * SS), int(200 * SS)), (0, 0, 0, 0))
            td = ImageDraw.Draw(tmp)
            td.text((1000 * SS, 100 * SS), s, font=font, fill=fill + (255,), anchor="mm")
            bbox = tmp.getbbox()
            if not bbox:
                return
            crop = tmp.crop(bbox)
            rot = crop.rotate(angle, expand=True)
            cx, cy = self.pt(x, y)
            self.img.paste(rot, (int(cx - rot.width / 2), int(cy - rot.height / 2)), rot)
            return
        self.d.text(self.pt(x, y), s, font=font, fill=fill, anchor=anchor)

    def finish(self) -> Image.Image:
        return self.img.resize((self.w, self.h), Image.LANCZOS)


# ---------------------------------------------------------------- 家具

def rect(s: Surface, x, y, w, h, fill=FURN, outline=FURN_LINE, width_cm=1.2, radius_cm=3):
    """通用柜体/台面：家具元组 ("rect", x, y, w, h, ...) 走这里。"""
    s.rect(x, y, w, h, fill=fill, outline=outline, width_cm=width_cm, radius_cm=radius_cm)


def bed(s: Surface, x, y, w, h):
    s.rect(x, y, w, h, fill=FURN, outline=FURN_LINE, width_cm=1.4, radius_cm=3)
    s.rect(x + 6, y + 6, w - 12, min(48, h * 0.22), fill=FURN_SOFT, outline=FURN_LINE, width_cm=1.0, radius_cm=4)
    s.line([(x + 6, y + h * 0.42), (x + w - 6, y + h * 0.42)], 0.8, FURN_LINE)


def wardrobe(s: Surface, x, y, w, h):
    s.rect(x, y, w, h, fill=FURN, outline=FURN_LINE, width_cm=1.2)
    s.line([(x + w / 2, y + 4), (x + w / 2, y + h - 4)], 0.8, FURN_LINE)


def sofa(s: Surface, x, y, w, h):
    s.rect(x, y, w, h, fill=FURN, outline=FURN_LINE, width_cm=1.4, radius_cm=5)
    s.rect(x + 4, y + 4, w - 8, max(10, h * 0.28), fill=FURN_SOFT, outline=None)
    mid = x + w / 2
    s.line([(mid, y + 6), (mid, y + h - 6)], 0.8, FURN_LINE)


def coffee_table(s: Surface, cx, cy, w, h):
    s.rect(cx - w / 2, cy - h / 2, w, h, fill=FURN, outline=FURN_LINE, width_cm=1.2, radius_cm=4)


def dining(s: Surface, cx, cy, w, h, chairs=4):
    s.rect(cx - w / 2, cy - h / 2, w, h, fill=FURN, outline=FURN_LINE, width_cm=1.4, radius_cm=6)
    r = 14
    if chairs >= 2:
        for sx in (-1, 1):
            s.circle(cx + sx * (w / 2 + r + 4), cy, r, fill=FURN, outline=FURN_LINE, width_cm=1.0)
    if chairs >= 4:
        for sy in (-1, 1):
            s.circle(cx, cy + sy * (h / 2 + r + 4), r, fill=FURN, outline=FURN_LINE, width_cm=1.0)


def kitchen_run(s: Surface, x, y, w, h, sink=True, stove=True):
    s.rect(x, y, w, h, fill=FURN, outline=FURN_LINE, width_cm=1.2, radius_cm=2)
    if sink:
        s.rect(x + w * 0.12, y + h * 0.25, w * 0.22, h * 0.5, fill=FURN_SOFT, outline=FURN_LINE, width_cm=0.8, radius_cm=2)
    if stove:
        cx = x + w * 0.66
        for dx, dy in ((-1, -1), (1, -1), (-1, 1), (1, 1)):
            s.circle(cx + dx * w * 0.07, y + h / 2 + dy * h * 0.16, min(w, h) * 0.07, outline=FURN_LINE, width_cm=0.9)


def wc(s: Surface, cx, cy):
    s.rect(cx - 18, cy - 26, 36, 14, fill=FURN_SOFT, outline=FURN_LINE, width_cm=1.0, radius_cm=4)
    s.circle(cx, cy + 4, 16, fill=FURN, outline=FURN_LINE, width_cm=1.0)


def basin(s: Surface, x, y, w=56, h=40):
    s.rect(x, y, w, h, fill=FURN, outline=FURN_LINE, width_cm=1.0, radius_cm=6)
    s.circle(x + w / 2, y + h / 2, min(w, h) * 0.3, outline=FURN_LINE, width_cm=0.8)


def shower(s: Surface, x, y, w, h):
    s.rect(x, y, w, h, fill=FURN, outline=FURN_LINE, width_cm=1.2)
    s.line([(x, y), (x + w, y + h)], 0.7, FURN_LINE)
    s.circle(x + w * 0.5, y + 10, 5, outline=FURN_LINE, width_cm=0.8)


def bathtub(s: Surface, x, y, w, h):
    s.rect(x, y, w, h, fill=FURN, outline=FURN_LINE, width_cm=1.2, radius_cm=12)


def desk(s: Surface, x, y, w, h, chair_side=1):
    s.rect(x, y, w, h, fill=FURN, outline=FURN_LINE, width_cm=1.2, radius_cm=2)
    s.circle(x + w / 2, y + chair_side * (h / 2 + 20), 14, fill=FURN, outline=FURN_LINE, width_cm=1.0)


def plant(s: Surface, cx, cy, r=16):
    s.circle(cx, cy, r, fill=(226, 240, 226), outline=(120, 160, 120), width_cm=1.0)
    s.circle(cx - r * 0.3, cy - r * 0.2, r * 0.45, fill=(206, 228, 206), outline=None)


def stairs(s: Surface, x, y, w, h, steps=8, along="x"):
    s.rect(x, y, w, h, fill=FURN, outline=FURN_LINE, width_cm=1.2)
    for i in range(1, steps):
        if along == "x":
            s.line([(x + w * i / steps, y), (x + w * i / steps, y + h)], 0.7, FURN_LINE)
        else:
            s.line([(x, y + h * i / steps), (x + w, y + h * i / steps)], 0.7, FURN_LINE)


# ---------------------------------------------------------------- 墙与开口

def unit(a, b):
    dx, dy = b[0] - a[0], b[1] - a[1]
    L = math.hypot(dx, dy) or 1.0
    return dx / L, dy / L, L


def draw_opening(s: Surface, a, b, t, op: Opening):
    ux, uy, L = unit(a, b)
    nx, ny = -uy, ux
    x0, y0 = a[0] + ux * op.offset, a[1] + uy * op.offset
    x1, y1 = x0 + ux * op.width, y0 + uy * op.width
    # 先把墙清掉（画白），再画开口符号
    s.line([(x0, y0), (x1, y1)], t + 6, fill=PAPER, cap="butt")
    if op.kind == "window":
        for k in (-1, 0, 1):
            off = k * t * 0.26
            s.line([(x0 + nx * off, y0 + ny * off), (x1 + nx * off, y1 + ny * off)], 1.6, WALL_LIGHT, cap="butt")
        s.line([(x0, y0), (x1, y1)], t * 0.8, fill=PAPER, cap="butt")
        for k in (-1, 1):
            off = k * t * 0.3
            s.line([(x0 + nx * off, y0 + ny * off), (x1 + nx * off, y1 + ny * off)], 1.8, WALL_LIGHT, cap="butt")
    elif op.kind == "door":
        # 门扇：绕起点转 90°，扫向 side 一侧
        hinge = (x0, y0)
        end = (x0 + nx * op.width * op.side, y0 + ny * op.width * op.side)
        s.line([hinge, end], 1.4, WALL_LIGHT)
        r = op.width
        # 注意：画布整体有 MARGIN 偏移，必须走 pt()（带偏移）而不是 px()，
        # 否则门弧会和墙、门扇错开一个边距的量——之前的门全都“飘”在房间里。
        x0p, y0p = s.pt(hinge[0] - r, hinge[1] - r)
        x1p, y1p = s.pt(hinge[0] + r, hinge[1] + r)
        bbox = [x0p, y0p, x1p, y1p]
        start = math.degrees(math.atan2(uy, ux))
        if op.side > 0:
            s.d.arc(bbox, start, start + 90, fill=WALL_LIGHT, width=max(1, int(s.px(0.8))))
        else:
            s.d.arc(bbox, start - 90, start, fill=WALL_LIGHT, width=max(1, int(s.px(0.8))))
    # pass：什么都不画，就是个洞


def draw_walls(s: Surface, panel: Panel):
    for i, (a, b, t) in enumerate(panel.walls):
        s.line([a, b], t, WALL)
    for op in panel.openings:
        a, b, t = panel.walls[op.wall]
        draw_opening(s, a, b, t, op)


def draw_rooms(s: Surface, panel: Panel):
    for r in panel.rooms:
        pts = r.poly if r.poly else [(r.x, r.y), (r.x + r.w, r.y), (r.x + r.w, r.y + r.h), (r.x, r.y + r.h)]
        s.poly(pts, fill=FLOOR.get(r.kind, (245, 245, 245)))


def room_center(r: Room):
    if r.poly:
        xs = [p[0] for p in r.poly]
        ys = [p[1] for p in r.poly]
        return (sum(xs) / len(xs) + r.label_dx, sum(ys) / len(ys) + r.label_dy)
    return (r.x + r.w / 2 + r.label_dx, r.y + r.h / 2 + r.label_dy)


def _label_plate(s: Surface, cx, cy, w_px, h_px):
    pad_x, pad_y = s.px(5), s.px(3)
    x, y = s.pt(cx, cy)
    s.d.rounded_rectangle(
        [x - w_px / 2 - pad_x, y - h_px / 2 - pad_y, x + w_px / 2 + pad_x, y + h_px / 2 + pad_y],
        radius=s.px(3), fill=(255, 255, 255, 232))


def draw_labels(s: Surface, panel: Panel, size_px=22):
    for r in panel.rooms:
        cx, cy = room_center(r)
        name_font = load_font(int(size_px * SS), True)
        area_font = load_font(int(size_px * 0.82 * SS), False)
        area = f"{r.area} ㎡"
        nb = name_font.getbbox(r.name)
        ab = area_font.getbbox(area)
        w_px = max(nb[2] - nb[0], ab[2] - ab[0])
        h_px = (nb[3] - nb[1]) + (ab[3] - ab[1]) + s.px(6)
        _label_plate(s, cx, cy, w_px, h_px)
        s.text(cx, cy - 9, r.name, size_px, bold=True)
        s.text(cx, cy + 13, area, size_px * 0.82, fill=(110, 110, 110))


# ---------------------------------------------------------------- 尺寸线

def draw_dim_chain(s: Surface, pts_cm, vertical: bool, anchor_cm: float, extent: float, size_px=17):
    """在 anchor_cm 处画一条链式尺寸线，pts_cm 是各分段点的坐标。"""
    if len(pts_cm) < 2:
        return
    offset = 34 if vertical else 30
    for p in pts_cm:
        if vertical:
            s.line([(extent, p), (anchor_cm, p)], 0.6, DIM, cap="butt")
        else:
            s.line([(p, extent), (p, anchor_cm)], 0.6, DIM, cap="butt")
    s.line([(anchor_cm, pts_cm[0]), (anchor_cm, pts_cm[-1])] if vertical else
           [(pts_cm[0], anchor_cm), (pts_cm[-1], anchor_cm)], 0.9, DIM, cap="butt")
    # 45° 起止符
    for p in (pts_cm[0], pts_cm[-1]):
        x, y = (anchor_cm, p) if vertical else (p, anchor_cm)
        s.line([(x - 4, y + 4), (x + 4, y - 4)], 1.6, DIM, cap="butt")
    for i in range(len(pts_cm) - 1):
        a, b = pts_cm[i], pts_cm[i + 1]
        mid = (a + b) / 2
        txt = f"{int(round(b - a))}"
        if vertical:
            s.text(anchor_cm - offset * 0.55, mid, txt, size_px, fill=(70, 70, 70), angle=90)
            s.line([(anchor_cm - 5, b), (anchor_cm + 5, b)], 0.8, DIM, cap="butt")
        else:
            s.text(mid, anchor_cm - 14, txt, size_px, fill=(70, 70, 70))
            s.line([(b, anchor_cm - 5), (b, anchor_cm + 5)], 0.8, DIM, cap="butt")


def north_arrow(s: Surface, x, y, r=26):
    s.circle(x, y, r, fill=None, outline=(150, 150, 150), width_cm=1.0)
    s.poly([(x, y - r * 0.8), (x - r * 0.32, y + r * 0.45), (x, y + r * 0.15), (x + r * 0.32, y + r * 0.45)],
           fill=(120, 120, 120))
    s.text(x, y + r * 1.9, "N", 15, fill=(120, 120, 120), bold=True)


def title_block(s: Surface, x, y, lines, size_px=17):
    for i, line in enumerate(lines):
        s.text(x, y + i * 22, line, size_px, fill=(90, 90, 90), anchor="rb")


# ---------------------------------------------------------------- 渲染

def render(plan: Plan, out: Path, scale: float | None = None):
    if scale is None:
        scale = TARGET_W / (plan.w + 2 * MARGIN_CM)
    s = Surface(plan.w + 2 * MARGIN_CM, plan.h + 2 * MARGIN_CM, scale)

    for panel in plan.panels:
        draw_rooms(s, panel)
        for item in panel.furniture:
            kind = item[0]
            globals()[kind](s, *item[1:])

    for panel in plan.panels:
        draw_walls(s, panel)
    for panel in plan.panels:
        draw_labels(s, panel)
        if panel.dims_x:
            if "top" in panel.dim_sides:
                draw_dim_chain(s, panel.dims_x, False, -26, 0)
            if "bottom" in panel.dim_sides:
                draw_dim_chain(s, panel.dims_x, False, plan.h + 34, plan.h)
        if panel.dims_y:
            if "left" in panel.dim_sides:
                draw_dim_chain(s, panel.dims_y, True, -30, 0)
            if "right" in panel.dim_sides:
                draw_dim_chain(s, panel.dims_y, True, plan.w + 34, plan.w)
        if panel.caption:
            xs = [pt[0] for w in panel.walls for pt in (w[0], w[1])]
            cx = (min(xs) + max(xs)) / 2
            s.text(cx, plan.h + 74, panel.caption, 20, fill=(90, 90, 90), bold=True)

    north_arrow(s, plan.w - 30, 40)
    title_block(s, plan.w + MARGIN_CM - 24, plan.h + MARGIN_CM - 20,
                ["智能家居课程设计 · 演示户型", plan.title])

    img = s.finish()
    out.parent.mkdir(parents=True, exist_ok=True)
    img.save(out, optimize=True)
    cm_per_px = 1.0 / scale
    # 定标点取「第一个面板」的外轮廓两个上角（复式有两块并排面板，用左边那块）
    xs = [pt[0] for w in plan.panels[0].walls for pt in (w[0], w[1])]
    ys = [pt[1] for w in plan.panels[0].walls for pt in (w[0], w[1])]
    x0, x1, y0 = min(xs), max(xs), min(ys)
    return {
        "file": out.name,
        "title": plan.title,
        "width_px": img.width,
        "height_px": img.height,
        "cm_per_px": round(cm_per_px, 6),
        "viewport_cm": [round(plan.w + 2 * MARGIN_CM, 1), round(plan.h + 2 * MARGIN_CM, 1)],
        "calibration": {
            "p1_px": [round((MARGIN_CM + x0) * scale, 2), round((MARGIN_CM + y0) * scale, 2)],
            "p2_px": [round((MARGIN_CM + x1) * scale, 2), round((MARGIN_CM + y0) * scale, 2)],
            "length_cm": round(x1 - x0, 1),
            "note": "p1/p2 是（第一块面板的）外轮廓左上、右上两角的像素坐标，真实距离等于该面板净宽",
        },
    }


# ---------------------------------------------------------------- 户型 1：一居 45 ㎡

def plan_one_bed() -> Plan:
    p = Panel()
    p.walls = [
        ((0, 0), (660, 0), 20),        # 0 上外墙
        ((660, 0), (660, 700), 20),    # 1 右外墙
        ((660, 700), (0, 700), 20),    # 2 下外墙
        ((0, 700), (0, 0), 20),        # 3 左外墙
        ((330, 0), (330, 700), 12),    # 4 中间隔墙
        ((330, 240), (660, 240), 12),  # 5 厨房/卫生间
        ((330, 400), (660, 400), 12),  # 6 卫生间/门厅
    ]
    p.openings = [
        Opening(0, 90, 170, "window"),
        Opening(0, 400, 170, "window"),
        Opening(1, 60, 140, "window"),
        Opening(1, 520, 95, "door", side=1),
        Opening(2, 120, 200, "window"),
        Opening(3, 430, 160, "window"),
        Opening(4, 240, 90, "door", side=1),
        Opening(4, 450, 130, "pass"),
        Opening(5, 150, 80, "door", side=1),
        Opening(6, 60, 80, "door", side=1),
    ]
    p.rooms = [
        Room("卧室", "bedroom", 0, 0, 330, 340),
        Room("客厅", "living", 0, 340, 330, 360),
        Room("厨房", "kitchen", 330, 0, 330, 240),
        Room("卫生间", "bath", 330, 240, 330, 160),
        Room("门厅", "hall", 330, 400, 330, 300),
    ]
    p.furniture = [
        ("bed", 55, 60, 175, 205),
        ("wardrobe", 40, 285, 180, 45),
        ("plant", 300, 40, 16),
        ("sofa", 25, 420, 205, 90),
        ("coffee_table", 128, 560, 110, 62),
        ("rect", 25, 640, 200, 38, FURN, FURN_LINE, 1.2, 3),
        ("plant", 300, 670, 18),
        ("kitchen_run", 344, 16, 300, 62),
        ("dining", 500, 168, 110, 70, 4),
        ("wc", 400, 352),
        ("basin", 344, 262, 62, 42),
        ("shower", 560, 262, 90, 120),
        ("rect", 344, 620, 120, 40, FURN, FURN_LINE, 1.2, 3),
        ("plant", 630, 660, 18),
    ]
    p.dims_x = [0, 330, 660]
    p.dims_y = [0, 240, 400, 700]
    return Plan("one-bed-45", "一居室 · 45 ㎡ · 6.6 × 7.0 m", [p], 660, 700)


# ---------------------------------------------------------------- 户型 2：两居 78 ㎡

def plan_two_bed() -> Plan:
    p = Panel()
    p.walls = [
        ((0, 0), (960, 0), 20),        # 0
        ((960, 0), (960, 820), 20),    # 1
        ((960, 820), (0, 820), 20),    # 2
        ((0, 820), (0, 0), 20),        # 3
        ((430, 0), (430, 820), 12),    # 4
        ((720, 0), (720, 240), 12),    # 5
        ((720, 240), (720, 820), 12),  # 6
        ((430, 360), (720, 360), 12),  # 7
        ((0, 470), (430, 470), 12),    # 8
    ]
    p.openings = [
        Opening(0, 90, 220, "window"),
        Opening(0, 520, 170, "window"),
        Opening(0, 770, 110, "window"),
        Opening(1, 520, 95, "door", side=1),
        Opening(2, 600, 200, "window"),
        Opening(3, 420, 200, "window"),
        Opening(3, 60, 160, "window"),
        Opening(4, 260, 90, "door", side=-1),
        Opening(4, 600, 150, "pass"),
        Opening(5, 150, 75, "door", side=1),
        Opening(6, 150, 90, "door", side=1),
        Opening(8, 250, 160, "pass"),
    ]
    p.rooms = [
        Room("客厅", "living", 0, 0, 430, 470),
        Room("餐厨", "kitchen", 0, 470, 430, 350),
        Room("主卧", "bedroom", 430, 0, 290, 360),
        Room("卫生间", "bath", 720, 0, 240, 240),
        Room("次卧", "bedroom", 430, 360, 290, 460),
        Room("走廊", "hall", 720, 240, 240, 580),
    ]
    p.furniture = [
        ("sofa", 40, 120, 210, 88),
        ("coffee_table", 150, 260, 120, 64),
        ("rect", 330, 100, 60, 200, FURN, FURN_LINE, 1.2, 3),
        ("plant", 380, 420, 18),
        ("kitchen_run", 20, 500, 300, 62),
        ("rect", 20, 620, 190, 40, FURN, FURN_LINE, 1.2, 3),
        ("dining", 150, 700, 130, 80, 6),
        ("bed", 470, 40, 180, 205),
        ("wardrobe", 600, 60, 45, 190),
        ("wc", 760, 90),
        ("basin", 730, 180, 62, 40),
        ("shower", 860, 100, 90, 110),
        ("bed", 470, 430, 160, 200),
        ("desk", 470, 700, 130, 55, -1),
        ("rect", 700, 560, 38, 130, FURN, FURN_LINE, 1.2, 3),
        ("rect", 740, 760, 130, 40, FURN, FURN_LINE, 1.2, 3),
    ]
    p.dims_x = [0, 430, 720, 960]
    p.dims_y = [0, 360, 470, 820]
    return Plan("two-bed-78", "两居室 · 78 ㎡ · 9.6 × 8.2 m", [p], 960, 820)


# ---------------------------------------------------------------- 户型 3：三居双卫 110 ㎡

def plan_three_bed() -> Plan:
    p = Panel()
    p.walls = [
        ((0, 0), (1140, 0), 20),         # 0
        ((1140, 0), (1140, 960), 20),    # 1
        ((1140, 960), (0, 960), 20),     # 2
        ((0, 960), (0, 0), 20),          # 3
        ((420, 0), (420, 960), 12),      # 4 左列/走廊
        ((600, 0), (600, 960), 12),      # 5 走廊/右列
        ((600, 360), (1140, 360), 12),   # 6 主卧下沿
        ((600, 580), (1140, 580), 12),   # 7 卫浴下沿
        ((840, 360), (840, 580), 12),    # 8 主卫/次卫隔墙
        ((0, 480), (420, 480), 12),      # 9 客厅/餐厨
    ]
    p.openings = [
        Opening(0, 80, 200, "window"),
        Opening(0, 660, 200, "window"),
        Opening(0, 960, 120, "window"),
        Opening(1, 120, 120, "window"),
        Opening(1, 430, 160, "window"),
        Opening(2, 200, 200, "window"),
        Opening(2, 820, 180, "window"),
        Opening(3, 120, 200, "window"),
        Opening(3, 560, 200, "window"),
        Opening(4, 240, 150, "pass"),
        Opening(5, 120, 90, "door", side=1),
        Opening(5, 450, 90, "door", side=1),
        Opening(5, 760, 90, "door", side=-1),
        Opening(6, 120, 90, "door", side=1),
        Opening(7, 180, 75, "door", side=1),
        Opening(8, 120, 75, "door", side=1),
        Opening(9, 220, 160, "pass"),
    ]
    p.rooms = [
        Room("客厅", "living", 0, 0, 420, 480),
        Room("餐厨", "kitchen", 0, 480, 420, 480),
        Room("走廊", "hall", 420, 0, 180, 960),
        Room("主卧", "bedroom", 600, 0, 540, 360),
        Room("主卫", "bath", 600, 360, 240, 220),
        Room("次卫", "bath", 840, 360, 300, 220),
        Room("次卧", "bedroom", 600, 580, 270, 380),
        Room("儿童房", "bedroom", 870, 580, 270, 380),
    ]
    p.furniture = [
        ("sofa", 60, 120, 230, 92),
        ("coffee_table", 175, 280, 120, 64),
        ("rect", 330, 80, 60, 220, FURN, FURN_LINE, 1.2, 3),
        ("plant", 370, 420, 18),
        ("kitchen_run", 20, 520, 360, 62),
        ("dining", 150, 720, 140, 86, 6),
        ("rect", 20, 860, 200, 40, FURN, FURN_LINE, 1.2, 3),
        ("bed", 640, 40, 185, 210),
        ("wardrobe", 950, 40, 45, 220),
        ("rect", 900, 60, 40, 200, FURN, FURN_LINE, 1.0, 2),
        ("wc", 660, 420),
        ("basin", 760, 380, 60, 40),
        ("shower", 700, 470, 90, 90),
        ("bathtub", 900, 380, 160, 80),
        ("basin", 880, 500, 60, 40),
        ("bed", 640, 620, 170, 200),
        ("desk", 640, 880, 130, 50, -1),
        ("bed", 910, 620, 150, 195),
        ("rect", 1060, 620, 40, 170, FURN, FURN_LINE, 1.2, 3),
    ]
    p.dims_x = [0, 420, 600, 840, 1140]
    p.dims_y = [0, 360, 580, 960]
    return Plan("three-bed-110", "三居双卫 · 110 ㎡ · 11.4 × 9.6 m", [p], 1140, 960)


# ---------------------------------------------------------------- 户型 4：L 形带斜墙 88 ㎡

def plan_l_shape() -> Plan:
    p = Panel()
    p.walls = [
        ((0, 0), (900, 0), 20),        # 0 上外墙
        ((900, 0), (900, 520), 20),    # 1 右外墙（上半）
        ((900, 520), (620, 520), 20),  # 2 凹口横墙
        ((620, 520), (620, 820), 20),  # 3 凹口竖墙
        ((620, 820), (0, 820), 20),    # 4 下外墙
        ((0, 820), (0, 0), 20),        # 5 左外墙
        ((430, 0), (430, 520), 12),    # 6 客厅/主卧
        ((0, 330), (430, 330), 12),    # 7 客厅/餐厅
        ((430, 520), (620, 520), 12),  # 8 主卧/书房
        ((300, 520), (300, 820), 12),  # 9 厨房/书房
    ]
    p.openings = [
        Opening(0, 80, 200, "window"),
        Opening(0, 560, 200, "window"),
        Opening(1, 180, 160, "window"),
        Opening(3, 100, 160, "window"),
        Opening(4, 120, 200, "window"),
        Opening(4, 460, 95, "door", side=1),
        Opening(5, 560, 200, "window"),
        Opening(6, 120, 90, "door", side=1),
        Opening(6, 400, 150, "pass"),
        Opening(7, 140, 160, "pass"),
        Opening(8, 60, 80, "door", side=1),
        Opening(9, 120, 90, "door", side=1),
    ]
    p.rooms = [
        Room("客厅", "living", 0, 0, 430, 330),
        Room("餐厅", "living", 0, 330, 430, 190),
        Room("主卧", "bedroom", 430, 0, 470, 520),
        Room("厨房", "kitchen", 0, 520, 300, 300),
        Room("书房", "study", 300, 520, 320, 300),
    ]
    p.furniture = [
        ("sofa", 40, 60, 210, 88),
        ("coffee_table", 150, 190, 120, 62),
        ("rect", 20, 270, 160, 40, FURN, FURN_LINE, 1.2, 3),
        ("plant", 390, 60, 18),
        ("dining", 200, 430, 130, 80, 4),
        ("bed", 470, 60, 185, 210),
        ("wardrobe", 800, 60, 45, 220),
        ("desk", 470, 430, 140, 55, -1),
        ("kitchen_run", 20, 560, 240, 60),
        ("rect", 20, 760, 200, 40, FURN, FURN_LINE, 1.2, 3),
        ("desk", 340, 600, 150, 60, -1),
        ("rect", 340, 720, 45, 150, FURN, FURN_LINE, 1.2, 3),
    ]
    p.dims_x = [0, 300, 430, 620, 900]
    p.dims_y = [0, 330, 520, 820]
    p.dim_sides = ("top", "left")
    return Plan("l-shape-88", "L 形异形 · 88 ㎡ · 9.0 × 8.2 m", [p], 900, 820)


# ---------------------------------------------------------------- 户型 5：复式 96 ㎡（上下两层并排）

def _duplex_floor(first: bool) -> Panel:
    """上下两层的平面形状相同：左边一大间 + 走廊楼梯，右边一间。"""
    p = Panel()
    p.walls = [
        ((0, 0), (620, 0), 20),      # 0 上外墙
        ((620, 0), (620, 700), 20),  # 1 右外墙
        ((620, 700), (0, 700), 20),  # 2 下外墙
        ((0, 700), (0, 0), 20),      # 3 左外墙
        ((0, 420), (620, 420), 12),  # 4 大间/走廊
        ((380, 0), (380, 700), 12),  # 5 左列/右列
    ]
    p.openings = [
        Opening(0, 60, 180, "window"),
        Opening(0, 430, 150, "window"),
        Opening(1, 150, 200, "window"),
        Opening(1, 520, 150, "window"),
        Opening(2, 330, 95, "door", side=-1) if first else Opening(2, 330, 140, "window"),
        Opening(3, 120, 200, "window"),
        Opening(3, 470, 150, "window"),
        Opening(4, 60, 90, "door", side=1),
        Opening(5, 300, 140, "pass") if first else Opening(5, 300, 90, "door", side=1),
    ]
    p.furniture = [
        ("stairs", 110, 470, 190, 180, 9, "y"),
        ("rect", 40, 640, 170, 40, FURN, FURN_LINE, 1.2, 3),
        ("plant", 340, 655, 18),
    ]
    if first:
        p.rooms = [
            Room("客厅", "living", 0, 0, 380, 420),
            Room("走廊", "hall", 0, 420, 380, 280),
            Room("餐厨", "kitchen", 380, 0, 240, 700),
        ]
        p.furniture += [
            ("sofa", 40, 60, 200, 88),
            ("coffee_table", 140, 200, 120, 62),
            ("rect", 20, 300, 160, 40, FURN, FURN_LINE, 1.2, 3),
            ("kitchen_run", 400, 20, 200, 60),
            ("dining", 500, 220, 110, 76, 4),
        ]
        p.caption = "1F 起居层"
    else:
        p.rooms = [
            Room("主卧", "bedroom", 0, 0, 380, 420),
            Room("走廊", "hall", 0, 420, 380, 280),
            Room("次卧", "bedroom", 380, 0, 240, 420),
            Room("书房", "study", 380, 420, 240, 280),
        ]
        p.furniture += [
            ("bed", 40, 60, 180, 205),
            ("wardrobe", 300, 60, 45, 200),
            ("bed", 420, 60, 160, 195),
            ("desk", 420, 300, 140, 55, -1),
            ("desk", 420, 500, 140, 55, -1),
        ]
        p.caption = "2F 卧室层"
    p.dims_x = [0, 380, 620]
    p.dims_y = [0, 420, 700]
    return p


def plan_duplex() -> Plan:
    a = _duplex_floor(True)
    b = _duplex_floor(False)
    b.dx = 780
    for i, w in enumerate(b.walls):
        b.walls[i] = ((w[0][0] + b.dx, w[0][1]), (w[1][0] + b.dx, w[1][1]), w[2])
    b.rooms = [Room(r.name, r.kind, r.x + b.dx, r.y, r.w, r.h) for r in b.rooms]
    furn = []
    for item in b.furniture:
        kind, args = item[0], list(item[1:])
        if kind in ("bed", "rect", "sofa", "desk", "kitchen_run", "stairs", "wardrobe", "plant"):
            args[0] += b.dx
        elif kind in ("dining", "coffee_table", "wc"):
            args[0] += b.dx
        furn.append(tuple([kind] + args))
    b.furniture = furn
    b.openings = [Opening(o.wall, o.offset, o.width, o.kind, o.side) for o in b.openings]
    b.dims_x = []
    b.dims_y = []
    return Plan("duplex-96", "复式 · 96 ㎡（两层各 48 ㎡）", [a, b], 780 + 620, 700)


# ---------------------------------------------------------------- 户型 6：办公大开间 180 ㎡

def plan_office() -> Plan:
    p = Panel()
    p.walls = [
        ((0, 0), (1400, 0), 20),
        ((1400, 0), (1400, 900), 20),
        ((1400, 900), (0, 900), 20),
        ((0, 900), (0, 0), 20),
        ((0, 560), (1400, 560), 12),
        ((300, 560), (300, 900), 12),
        ((600, 560), (600, 900), 12),
        ((840, 560), (840, 900), 12),
        ((1040, 560), (1040, 900), 12),
    ]
    p.openings = [
        Opening(0, 120, 260, "window"),
        Opening(0, 700, 260, "window"),
        Opening(1, 200, 220, "window"),
        Opening(1, 520, 95, "door", side=1),
        Opening(2, 200, 260, "window"),
        Opening(3, 260, 200, "window"),
        Opening(4, 150, 90, "door", side=1),
        Opening(4, 420, 90, "door", side=1),
        Opening(4, 680, 90, "door", side=1),
        Opening(4, 900, 90, "door", side=-1),
        Opening(4, 1180, 130, "pass"),
    ]
    p.rooms = [
        Room("开放办公区", "office", 0, 0, 1400, 560, label_dy=-150),
        Room("会议室 A", "office", 0, 560, 300, 340),
        Room("会议室 B", "office", 300, 560, 300, 340),
        Room("茶水间", "kitchen", 600, 560, 240, 340),
        Room("洗手间", "bath", 840, 560, 200, 340),
        Room("前台", "hall", 1040, 560, 360, 340),
    ]
    p.furniture = [
        ("desk", 60, 80, 150, 60, 1), ("desk", 60, 180, 150, 60, 1),
        ("desk", 60, 280, 150, 60, 1), ("desk", 60, 380, 150, 60, 1),
        ("desk", 400, 80, 150, 60, -1), ("desk", 400, 180, 150, 60, -1),
        ("desk", 400, 280, 150, 60, -1), ("desk", 400, 380, 150, 60, -1),
        ("desk", 760, 80, 150, 60, 1), ("desk", 760, 180, 150, 60, 1),
        ("desk", 760, 280, 150, 60, 1), ("desk", 760, 380, 150, 60, 1),
        ("desk", 1100, 80, 150, 60, -1), ("desk", 1100, 180, 150, 60, -1),
        ("desk", 1100, 280, 150, 60, -1), ("desk", 1100, 380, 150, 60, -1),
        ("dining", 150, 700, 150, 90, 6),
        ("dining", 450, 700, 150, 90, 6),
        ("kitchen_run", 620, 580, 200, 60),
        ("rect", 620, 820, 180, 40, FURN, FURN_LINE, 1.2, 3),
        ("wc", 900, 800), ("basin", 860, 620, 60, 42),
        ("rect", 1080, 720, 40, 160, FURN, FURN_LINE, 1.2, 3),
        ("plant", 1330, 620, 20), ("plant", 1330, 840, 20),
        ("plant", 40, 520, 18),
    ]
    p.dims_x = [0, 300, 600, 840, 1040, 1400]
    p.dims_y = [0, 560, 900]
    return Plan("office-180", "办公大开间 · 180 ㎡ · 14.0 × 9.0 m", [p], 1400, 900)


PLANS = [plan_one_bed, plan_two_bed, plan_three_bed, plan_l_shape, plan_duplex, plan_office]


def main():
    out_dir = Path(__file__).resolve().parent.parent / "frontend/public/underlays/samples/synthetic"
    manifest = []
    for fn in PLANS:
        plan = fn()
        rec = render(plan, out_dir / f"{plan.name}.png")
        manifest.append(rec)
        print(f"  {rec['file']:20s} {rec['width_px']}x{rec['height_px']}  {rec['cm_per_px']:.4f} cm/px")
    (out_dir / "index.json").write_text(
        json.dumps({"generated_by": "scripts/gen_underlay_samples.py", "plans": manifest},
                   ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(f"wrote {len(manifest)} plans to {out_dir}")


if __name__ == "__main__":
    main()
