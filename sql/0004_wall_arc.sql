-- 弧墙 v1：墙多存一个弧度，半径与圆心由弦长和弧角反解，不落库。
-- 0 表示直墙；正负表示往弦的哪一侧弯（逆时针为正）。
ALTER TABLE wall
  ADD COLUMN arc_extent double precision NOT NULL DEFAULT 0
  CHECK (arc_extent > -180 AND arc_extent < 180);
