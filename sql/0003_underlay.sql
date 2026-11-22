-- 底图：垫在画布下面照着描的户型图。
--
-- 单行表：底图是"这一份户型"的属性，不需要多张、也不需要版本历史（换图是硬操作，
-- 前端会清空撤销栈）。用 boolean 主键 + CHECK 保证永远只有一行。
CREATE TABLE plan_underlay (
    id           boolean     PRIMARY KEY DEFAULT true CHECK (id),
    content_type text        NOT NULL
        CHECK (content_type IN ('image/png', 'image/jpeg', 'image/webp')),
    width_px     integer     NOT NULL CHECK (width_px > 0 AND width_px <= 8000),
    height_px    integer     NOT NULL CHECK (height_px > 0 AND height_px <= 8000),
    bytes        bytea       NOT NULL,
    sha256       text        NOT NULL,
    -- 一个像素等于多少厘米：定标算出来的比例，默认 1 表示还没定标。
    cm_per_px    double precision NOT NULL CHECK (cm_per_px > 0),
    -- 图片左上角在世界坐标里的位置（旋转绕图片中心，不改这个点）。
    offset_x_cm  double precision NOT NULL,
    offset_y_cm  double precision NOT NULL,
    rotation_deg integer     NOT NULL DEFAULT 0 CHECK (rotation_deg IN (0, 90, 180, 270)),
    opacity      double precision NOT NULL DEFAULT 0.45 CHECK (opacity >= 0.1 AND opacity <= 1),
    visible      boolean     NOT NULL DEFAULT true,
    updated_at   timestamptz NOT NULL DEFAULT now()
);
