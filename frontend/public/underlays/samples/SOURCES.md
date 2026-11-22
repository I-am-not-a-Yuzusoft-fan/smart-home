# 底图示例素材出处

`frontend/public/underlays/samples/` 下的 5 张真实户型图全部来自 Wikimedia Commons，许可均为 **Public Domain**（公有领域），可随仓库再分发。

部分文件为方便加载做过等比缩放与格式转换（PNG → JPEG、长边 1280），转换不产生新的著作权，仍属公有领域。

| 文件 | 图纸 | 作者 / 绘制者 | 年代 | 原始尺寸 | 来源 |
| --- | --- | --- | --- | --- | --- |
| `massachusetts-ave-1785.jpg` | Typical Floor Plan—Apartment House at 1785 Massachusetts Avenue（华盛顿特区公寓标准层，含弧形墙与斜墙） | Jules Henri de Sibour（建筑师） | 1922 | 2196×1755 | [Commons](https://commons.wikimedia.org/wiki/File:Typical_Floor_Plan%E2%80%94Apartment_House_at_1785_Massachusetts_Avenue.jpg) |
| `nara-dwelling-1f.jpg` | Dwelling, 1st Floor Plan（美国灯塔管理局住宅一层） | 佚名（美国商务部灯塔局档案，NARA 79437523） | 1793–1939 | 4848×7194 | [Commons](https://commons.wikimedia.org/wiki/File:Dwelling,_,_1st_Floor_Plan_-_NARA_-_79437523.jpg) |
| `ballymun-flat-1965.jpg` | Typical floor plan of a Ballymun flat（都柏林 Ballymun 公寓标准层） | National Building Agency | 1965 | 1878×1244 | [Commons](https://commons.wikimedia.org/wiki/File:Typical_floor_plan_of_a_Ballymun_flat,_drawn_1965.png) |
| `caroline-court-vancouver.png` | Caroline Court Apartments Vancouver floor plan（温哥华小公寓） | J. P. Matheson & Son（建筑师） | 1913 | 416×661 | [Commons](https://commons.wikimedia.org/wiki/File:Caroline_Court_Apartments_Vancouver_floor_plan.png) |
| `hampton-court-1912.png` | Hampton Court Apt Vancouver floor plan（温哥华板式公寓，横向长条） | Grant, Henderson, and Cook（建筑师） | 1912 | 1457×571 | [Commons](https://commons.wikimedia.org/wiki/File:Hampton_Court_Apt_Vancouver_floor_plan.png) |

## 自产合成图（`synthetic/`）

现代 CAD 风格、非扫描件，由 `scripts/gen_underlay_samples.py` 生成（本仓库自产，CC0 无所谓，随便用）。共 6 张：一居 45 ㎡、两居 78 ㎡、三居双卫 110 ㎡、L 形异形 88 ㎡、复式 96 ㎡、办公大开间 180 ㎡。

比例精确已知，`synthetic/index.json` 给每张图记了 `cm_per_px` 与一组定标点，定标测试直接用它断言误差 < 0.1%。详见 `synthetic/README.md`。

## 使用注意

- 底图只是参照物：房间面积与墙长仍由用户画的墙决定，底图不参与任何几何推导。
- 真实图纸里的弧形墙、圆形柱无法用现有直线墙表达，只能画折线近似；`massachusetts-ave-1785.jpg` 刻意保留弧形墙，用来演示这一局限。
