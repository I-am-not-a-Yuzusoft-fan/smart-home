-- 0002 墙面挂件：item_type.mount + item.(wall_id, wall_offset)
--
-- 门窗（opening）本来就是「墙 + 沿墙偏移」，这一版把挂墙物品也统一到同一套语义：
--   位置 = 墙上 A 端点出发 offset 厘米处，朝向 = 墙的角度。
-- 因此挂墙物品的 x/y/rotation 是**派生值**：写入时由服务端按墙算出来存下，
-- 墙被拖动或改长时由服务端重算，前端只读不算。
--
-- 删墙的取舍（issue #8 二选一）：级联删除。开口与挂墙物品都随墙消失，
-- 前端在删除前统计数量并提示「这面墙上还有 N 个开口、M 件挂件」。
-- 这样库里永远不会出现指向不存在墙的行（验收要求 wall_id IS NULL 查询返回 0）。

ALTER TABLE item_type
    ADD COLUMN mount text NOT NULL DEFAULT 'floor'
    CONSTRAINT item_type_mount_enum CHECK (mount IN ('floor', 'wall', 'ceiling'));

ALTER TABLE item
    ADD COLUMN wall_id uuid REFERENCES wall (id) ON DELETE CASCADE,
    ADD COLUMN wall_offset double precision
        CONSTRAINT item_wall_offset_nonnegative CHECK (wall_offset >= 0);

-- 两个字段必须同时有或同时无：只有墙没有偏移等于没定位。
ALTER TABLE item
    ADD CONSTRAINT item_wall_fields_together
    CHECK ((wall_id IS NULL) = (wall_offset IS NULL));

CREATE INDEX item_wall_id_idx ON item (wall_id) WHERE wall_id IS NOT NULL;
CREATE INDEX opening_wall_id_idx ON opening (wall_id);

-- 演示数据里只有窗帘贴墙、吸顶灯在天花板，其余都落地。
UPDATE item_type SET mount = 'wall' WHERE key = 'curtain';
UPDATE item_type SET mount = 'ceiling' WHERE key = 'ceiling_light';
