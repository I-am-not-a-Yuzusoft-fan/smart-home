-- seed.sql —— 演示数据（固定 UUID，保证「migrate down → up → seed」十秒重建后完全一致）
-- 用法：见 scripts/reset.sh
-- 说明：能力元数据的 capabilities 数组是所有层的唯一来源；改这里即可新增类型，不改前端代码。

BEGIN;

-- 账号：两种角色各一人（访客走只读大屏 /wall，不占账号）
INSERT INTO app_user (id, username, display_name, password_hash, role) VALUES
  ('00000000-0000-4000-8000-000000000001', 'zhang', '张先生', '$2a$12$vCKdg0kYy.teFm0UFH6GIux0GDDz3tGuGNg2P8MI29OXdZq4ErO/O', 'admin'),
  ('00000000-0000-4000-8000-000000000002', 'li',    '李女士', '$2a$12$vCKdg0kYy.teFm0UFH6GIux0GDDz3tGuGNg2P8MI29OXdZq4ErO/O', 'member');

-- 类型模板（家具 2 个 + 设备 8 个）
INSERT INTO item_type (key, kind, name, icons, default_width, default_depth, capabilities, driver) VALUES
  ('sofa', 'furniture', '沙发', 'sofa', 180, 80, '[]'::jsonb, NULL),
  ('table', 'furniture', '茶几', 'table', 90, 50, '[]'::jsonb, NULL),
  ('ceiling_light', 'device', '吸顶灯', 'light', 40, 40,
   '[{"key":"on","name":"开关","type":"bool","writable":true,"history":true},
     {"key":"brightness","name":"亮度","type":"number","unit":"%","min":0,"max":100,"step":1,"writable":true,"history":true},
     {"key":"color_temp","name":"色温","type":"number","unit":"K","min":2700,"max":6500,"step":100,"writable":true,"history":true}]'::jsonb, 'simulator'),
  ('lamp', 'device', '床头灯', 'lamp', 20, 20,
   '[{"key":"on","name":"开关","type":"bool","writable":true,"history":true},
     {"key":"brightness","name":"亮度","type":"number","unit":"%","min":0,"max":100,"step":1,"writable":true,"history":true},
     {"key":"color_temp","name":"色温","type":"number","unit":"K","min":2700,"max":6500,"step":100,"writable":true,"history":true}]'::jsonb, 'simulator'),
  ('ac', 'device', '空调', 'ac', 60, 25,
   '[{"key":"on","name":"开关","type":"bool","writable":true,"history":true},
     {"key":"mode","name":"模式","type":"enum","options":["cool","heat","fan"],"writable":true,"history":true},
     {"key":"target_temp","name":"目标温度","type":"number","unit":"℃","min":16,"max":30,"step":0.5,"writable":true,"history":true},
     {"key":"fan_speed","name":"风速","type":"enum","options":["low","mid","high","auto"],"writable":true,"history":true}]'::jsonb, 'simulator'),
  ('curtain', 'device', '窗帘', 'curtain', 200, 10,
   '[{"key":"position","name":"开合度","type":"number","unit":"%","min":0,"max":100,"step":1,"writable":true,"history":true}]'::jsonb, 'simulator'),
  ('humidifier', 'device', '加湿器', 'humidifier', 25, 25,
   '[{"key":"on","name":"开关","type":"bool","writable":true,"history":true},
     {"key":"level","name":"档位","type":"enum","options":["low","mid","high"],"writable":true,"history":true}]'::jsonb, 'simulator'),
  ('purifier', 'device', '空气净化器', 'purifier', 30, 30,
   '[{"key":"on","name":"开关","type":"bool","writable":true,"history":true},
     {"key":"level","name":"档位","type":"enum","options":["low","mid","high","auto"],"writable":true,"history":true}]'::jsonb, 'simulator'),
  ('sensor_th', 'device', '温湿度传感器', 'sensor', 8, 8,
   '[{"key":"temperature","name":"温度","type":"number","unit":"℃","min":-10,"max":50,"writable":false,"history":true},
     {"key":"humidity","name":"湿度","type":"number","unit":"%RH","min":0,"max":100,"writable":false,"history":true}]'::jsonb, 'simulator'),
  ('sensor_lux', 'device', '光照传感器', 'sensor', 8, 8,
   '[{"key":"illuminance","name":"照度","type":"number","unit":"lux","min":0,"max":2000,"writable":false,"history":true}]'::jsonb, 'simulator'),
  ('sensor_pir', 'device', '人体存在传感器', 'sensor', 8, 8,
   '[{"key":"occupied","name":"有人","type":"bool","writable":false,"history":true}]'::jsonb, 'simulator'),
  ('door_sensor', 'device', '门磁', 'sensor', 6, 6,
   '[{"key":"open","name":"开合","type":"bool","writable":false,"history":true}]'::jsonb, 'simulator');

-- 平面图：一个 400×300 cm 的闭合矩形（客厅），墙端点被相邻墙共享
INSERT INTO corner (id, x, y) VALUES
  ('00000000-0000-4000-8000-000000000101',   0,   0),
  ('00000000-0000-4000-8000-000000000102', 400,   0),
  ('00000000-0000-4000-8000-000000000103', 400, 300),
  ('00000000-0000-4000-8000-000000000104',   0, 300);

INSERT INTO wall (id, a, b) VALUES
  ('00000000-0000-4000-8000-000000000201', '00000000-0000-4000-8000-000000000101', '00000000-0000-4000-8000-000000000102'),
  ('00000000-0000-4000-8000-000000000202', '00000000-0000-4000-8000-000000000102', '00000000-0000-4000-8000-000000000103'),
  ('00000000-0000-4000-8000-000000000203', '00000000-0000-4000-8000-000000000103', '00000000-0000-4000-8000-000000000104'),
  ('00000000-0000-4000-8000-000000000204', '00000000-0000-4000-8000-000000000104', '00000000-0000-4000-8000-000000000101');

INSERT INTO opening (wall_id, kind, "offset", width, height, sill) VALUES
  ('00000000-0000-4000-8000-000000000201', 'door',   350, 90, 210, 0),
  ('00000000-0000-4000-8000-000000000202', 'window',  80, 150, 120, 90);

-- 房间命名：geometry 由墙推导，这里只存名字（主键是环多边形的哈希）
INSERT INTO room_name (polygon_hash, name, kind) VALUES
  ('2a6c15cc655eea79', '客厅', 'living');

-- 物品：家具与设备同表；坐标为平面坐标（cm）
INSERT INTO item (id, type_key, name, room_hash, x, y, rotation) VALUES
  ('00000000-0000-4000-8000-000000000301', 'ceiling_light', '客厅主灯', '2a6c15cc655eea79', 200, 150, 0),
  ('00000000-0000-4000-8000-000000000302', 'ac',            '客厅空调', '2a6c15cc655eea79', 260,  20, 0),
  ('00000000-0000-4000-8000-000000000303', 'curtain',       '客厅窗帘', '2a6c15cc655eea79', 200, 290, 0),
  ('00000000-0000-4000-8000-000000000304', 'sensor_th',     '客厅温湿度', '2a6c15cc655eea79', 380, 250, 0),
  ('00000000-0000-4000-8000-000000000305', 'sensor_lux',    '客厅光照',  '2a6c15cc655eea79', 380, 150, 0),
  ('00000000-0000-4000-8000-000000000306', 'sensor_pir',    '客厅人体',  '2a6c15cc655eea79', 200,  40, 0),
  ('00000000-0000-4000-8000-000000000401', 'sofa',          '三人沙发',  '2a6c15cc655eea79', 120, 120, 0),
  ('00000000-0000-4000-8000-000000000402', 'table',         '茶几',      '2a6c15cc655eea79', 120, 200, 0)
ON CONFLICT (id) DO NOTHING;

-- 初始状态：设备当前值与只读传感器的读数
INSERT INTO capability_state (item_id, capability_key, value, source, updated_at) VALUES
  ('00000000-0000-4000-8000-000000000301', 'on',          'false'::jsonb,  'device', now()),
  ('00000000-0000-4000-8000-000000000301', 'brightness',  '0'::jsonb,      'device', now()),
  ('00000000-0000-4000-8000-000000000301', 'color_temp',  '4000'::jsonb,   'device', now()),
  ('00000000-0000-4000-8000-000000000302', 'on',          'false'::jsonb,  'device', now()),
  ('00000000-0000-4000-8000-000000000302', 'mode',        '"cool"'::jsonb, 'device', now()),
  ('00000000-0000-4000-8000-000000000302', 'target_temp', '26'::jsonb,     'device', now()),
  ('00000000-0000-4000-8000-000000000302', 'fan_speed',   '"auto"'::jsonb, 'device', now()),
  ('00000000-0000-4000-8000-000000000303', 'position',    '0'::jsonb,      'device', now()),
  ('00000000-0000-4000-8000-000000000304', 'temperature', '31.5'::jsonb,   'device', now()),
  ('00000000-0000-4000-8000-000000000304', 'humidity',    '68'::jsonb,     'device', now()),
  ('00000000-0000-4000-8000-000000000305', 'illuminance', '12000'::jsonb,  'device', now()),
  ('00000000-0000-4000-8000-000000000306', 'occupied',    'true'::jsonb,   'device', now())
ON CONFLICT (item_id, capability_key) DO UPDATE
SET value = EXCLUDED.value, source = EXCLUDED.source, updated_at = EXCLUDED.updated_at;

-- 一个场景：回家（脚本语义——有序步骤、带延时、有守卫）
INSERT INTO mode (id, name, definition, run_mode, created_by) VALUES
  ('00000000-0000-4000-8000-000000000501', '回家', '{
     "version": 1,
     "steps": [
       {"nodeId":"s1","type":"action","alias":"开客厅灯","itemId":"00000000-0000-4000-8000-000000000301","capability":"on","value":true},
       {"nodeId":"s2","type":"action","alias":"灯调到 70%","itemId":"00000000-0000-4000-8000-000000000301","capability":"brightness","value":70},
       {"nodeId":"s3","type":"delay","alias":"等 2 秒","ms":2000},
       {"nodeId":"s4","type":"action","alias":"关窗帘","itemId":"00000000-0000-4000-8000-000000000303","capability":"position","value":0},
       {"nodeId":"s5","type":"choose","alias":"如果室温高于 28℃","when":{"all":[{"itemId":"00000000-0000-4000-8000-000000000304","capability":"temperature","op":">","value":28}]},
        "then":[{"nodeId":"s5a","type":"action","alias":"开空调","itemId":"00000000-0000-4000-8000-000000000302","capability":"on","value":true}],
        "else":[]}
     ]
   }'::jsonb, 'restart', '00000000-0000-4000-8000-000000000001');

COMMIT;
