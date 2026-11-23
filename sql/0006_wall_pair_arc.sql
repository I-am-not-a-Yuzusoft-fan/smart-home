-- 同一对端点上允许「直墙 + 弧墙」共存：半圆阳台就是这种形状
-- （弦上那面直墙带门，外面再挂一段半圆弧）。
-- 唯一性因此从 (a, b) 放宽到 (a, b, arc_extent)：几何完全相同的墙仍然只有一面。
ALTER TABLE wall DROP CONSTRAINT IF EXISTS wall_pair_unique;
DROP INDEX IF EXISTS wall_pair_uniq;
CREATE UNIQUE INDEX wall_pair_arc_uniq ON wall (LEAST(a, b), GREATEST(a, b), arc_extent);
