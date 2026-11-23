-- 半圆是合法弧墙：把弧度的开区间改成闭区间，正好 180° 也能存。
ALTER TABLE wall DROP CONSTRAINT wall_arc_extent_check;
ALTER TABLE wall
  ADD CONSTRAINT wall_arc_extent_check CHECK (arc_extent >= -180 AND arc_extent <= 180);
