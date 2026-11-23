-- 阳台/凸窗也是房间：用途枚举补一个 balcony。
ALTER TABLE room_name DROP CONSTRAINT IF EXISTS room_name_kind_check;
ALTER TABLE room_name ADD CONSTRAINT room_name_kind_check
    CHECK (kind IN ('living', 'bedroom', 'kitchen', 'bathroom', 'study', 'hallway', 'balcony'));
