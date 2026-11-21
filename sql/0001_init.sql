-- 0001_init.sql —— 智能家居系统初始表结构
-- 依据：docs/deliverables/03-概要设计说明书.md §5、docs/adr/0002-数据模型与分层.md
-- 约定：
--   * 所有时间列用 timestamptz（避免容器 UTC / 前端 UTC+8 不一致）
--   * 身份键用不可复用的 uuid v4；只在日志表用 bigserial
--   * updated_at 由写入语句显式赋值，不使用触发器（触发器在 sqlc 生成代码里不可见）
--   * 迁移只做 additive：本文件一经合并，不再修改，后续变更另建 000N_*.sql

BEGIN;

-- ── 平面图几何 ──────────────────────────────────────────────
-- 房间不存几何：由墙的闭环推导（见 ADR 0002 §2.1），此处只存端点与墙。

CREATE TABLE corner (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    x           double precision NOT NULL,
    y           double precision NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE corner IS '墙端点；多面墙共享同一端点，保证闭合判定为精确相等而非浮点近似';

CREATE TABLE wall (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    a           uuid NOT NULL REFERENCES corner (id) ON DELETE RESTRICT,
    b           uuid NOT NULL REFERENCES corner (id) ON DELETE RESTRICT,
    thickness   double precision NOT NULL DEFAULT 10 CHECK (thickness > 0),
    height      double precision NOT NULL DEFAULT 300 CHECK (height > 0),
    created_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT wall_endpoints_distinct CHECK (a <> b),
    CONSTRAINT wall_pair_unique UNIQUE (a, b)
);
COMMENT ON TABLE wall IS '墙体（1 单位 = 1 cm）；端点方向由应用层归一化后再写入';

CREATE TABLE opening (
    id       uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    wall_id  uuid NOT NULL REFERENCES wall (id) ON DELETE CASCADE,
    kind     text NOT NULL CHECK (kind IN ('door', 'window')),
    "offset" double precision NOT NULL CHECK ("offset" >= 0),
    width    double precision NOT NULL CHECK (width > 0),
    height   double precision NOT NULL CHECK (height > 0),
    sill     double precision NOT NULL DEFAULT 0 CHECK (sill >= 0),
    created_at timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE opening IS '门窗：沿墙 "offset" 定位（不存绝对坐标），墙移动时自动跟随';

CREATE INDEX opening_wall_idx ON opening (wall_id);

CREATE TABLE room_name (
    polygon_hash  text PRIMARY KEY,
    name          text NOT NULL,
    kind          text NOT NULL CHECK (kind IN ('living', 'bedroom', 'kitchen', 'bathroom', 'study', 'hallway')),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE room_name IS '房间命名表：主键是墙环多边形的哈希；房间几何不落库，墙改则哈希变、旧命名失效';

-- ── 类型与物品 ──────────────────────────────────────────────
CREATE TABLE item_type (
    key                   text PRIMARY KEY,
    kind                  text NOT NULL CHECK (kind IN ('furniture', 'device')),
    name                  text NOT NULL,
    icons                 text,
    default_width         double precision NOT NULL DEFAULT 60 CHECK (default_width > 0),
    default_depth         double precision NOT NULL DEFAULT 60 CHECK (default_depth > 0),
    capabilities          jsonb NOT NULL DEFAULT '[]'::jsonb,
    capabilities_version  integer NOT NULL DEFAULT 1 CHECK (capabilities_version > 0),
    driver                text,
    created_at            timestamptz NOT NULL DEFAULT now(),
    updated_at            timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT item_type_capabilities_is_array CHECK (jsonb_typeof(capabilities) = 'array'),
    CONSTRAINT item_type_driver_only_device CHECK (driver IS NULL OR kind = 'device')
);
COMMENT ON TABLE item_type IS '类型模板：capabilities 是能力元数据数组，同时驱动前端表单、后端校验、模拟器行为、图标映射';

CREATE TABLE item (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    type_key    text NOT NULL REFERENCES item_type (key) ON DELETE RESTRICT,
    name        text NOT NULL,
    room_hash   text REFERENCES room_name (polygon_hash) ON DELETE SET NULL,
    x           double precision NOT NULL DEFAULT 0,
    y           double precision NOT NULL DEFAULT 0,
    rotation    double precision NOT NULL DEFAULT 0,
    status      text,
    attrs       jsonb NOT NULL DEFAULT '{}'::jsonb,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT item_status_enum CHECK (status IS NULL OR status IN ('normal', 'idle', 'maintenance')),
    CONSTRAINT item_attrs_is_object CHECK (jsonb_typeof(attrs) = 'object')
);
COMMENT ON TABLE item IS '家具与设备同表：设备=带能力的物品；status 只对 kind=furniture 有语义，设备状态一律在 capability_state';
COMMENT ON COLUMN item.status IS '仅家具使用（正常/闲置/维修）；设备不读此列，避免第二份真相';

CREATE INDEX item_type_idx ON item (type_key);
CREATE INDEX item_room_idx ON item (room_hash);

-- ── 账号 ────────────────────────────────────────────────────
CREATE TABLE app_user (
    id             uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    username       text NOT NULL UNIQUE,
    display_name   text NOT NULL,
    password_hash  text NOT NULL,
    role           text NOT NULL CHECK (role IN ('admin', 'member')),
    created_at     timestamptz NOT NULL DEFAULT now()
);
COMMENT ON TABLE app_user IS '家人账号：admin 管配置，member 只操作；访客走只读大屏 /wall，不占账号';

-- ── 状态与历史 ──────────────────────────────────────────────
CREATE TABLE capability_state (
    item_id         uuid NOT NULL REFERENCES item (id) ON DELETE CASCADE,
    capability_key  text NOT NULL,
    value           jsonb NOT NULL,
    source          text NOT NULL CHECK (source IN ('manual', 'automation', 'device', 'timeout')),
    updated_at      timestamptz NOT NULL,
    PRIMARY KEY (item_id, capability_key)
);
COMMENT ON TABLE capability_state IS '当前值：全系统唯一写入点（M-Ct-04）；主键即命令寻址单位 (itemId, capability)';
COMMENT ON COLUMN capability_state.capability_key IS '发布后不得重命名；改键即破坏历史与前端约定';

CREATE TABLE state_history (
    id              bigserial PRIMARY KEY,
    item_id         uuid NOT NULL,
    capability_key  text NOT NULL,
    value           jsonb NOT NULL,
    source          text NOT NULL CHECK (source IN ('manual', 'automation', 'device', 'timeout')),
    recorded_at     timestamptz NOT NULL
);
COMMENT ON TABLE state_history IS '只记变化，不记采样；不建 item 外键（历史不随设备删除而消失），保留 30 天后按天清理';

CREATE INDEX state_history_lookup_idx ON state_history (item_id, capability_key, recorded_at DESC);
CREATE INDEX state_history_time_idx ON state_history (recorded_at);

CREATE TABLE command_log (
    id              bigserial PRIMARY KEY,
    request_id      uuid NOT NULL UNIQUE,
    item_id         uuid,
    capability_key  text,
    target          jsonb,
    status          text NOT NULL CHECK (status IN ('ok', 'failed', 'timeout', 'rejected')),
    error           text,
    source          text NOT NULL CHECK (source IN ('manual', 'automation', 'device', 'timeout')),
    created_by      uuid REFERENCES app_user (id) ON DELETE SET NULL,
    created_at      timestamptz NOT NULL DEFAULT now(),
    completed_at    timestamptz,
    duration_ms     integer CHECK (duration_ms >= 0)
);
COMMENT ON TABLE command_log IS '命令台账：失败的命令也在此留痕（不写 state_history）；request_id 唯一即幂等键；保留 90 天';

CREATE INDEX command_log_time_idx ON command_log (created_at DESC);
CREATE INDEX command_log_item_idx ON command_log (item_id, created_at DESC);

-- ── 场景（F5） ──────────────────────────────────────────────
CREATE TABLE mode (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL UNIQUE,
    definition  jsonb NOT NULL,
    run_mode    text NOT NULL DEFAULT 'restart' CHECK (run_mode IN ('single', 'restart', 'queued', 'parallel')),
    enabled     boolean NOT NULL DEFAULT true,
    created_by  uuid REFERENCES app_user (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT mode_definition_is_object CHECK (jsonb_typeof(definition) = 'object')
);
COMMENT ON TABLE mode IS '场景：definition 是步骤树的整棵树 JSONB（编辑器保存/撤销/复制都是一次写）';

CREATE TABLE run (
    id                   uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    mode_id              uuid REFERENCES mode (id) ON DELETE SET NULL,
    status               text NOT NULL CHECK (status IN ('running', 'ok', 'failed', 'cancelled')),
    definition_snapshot  jsonb NOT NULL,
    triggered_by         uuid REFERENCES app_user (id) ON DELETE SET NULL,
    source               text NOT NULL CHECK (source IN ('manual', 'automation', 'device', 'timeout')),
    started_at           timestamptz NOT NULL DEFAULT now(),
    finished_at          timestamptz
);
COMMENT ON TABLE run IS '一次场景执行：definition_snapshot 固定本次执行的树，之后改场景不影响历史';

CREATE INDEX run_mode_idx ON run (mode_id, started_at DESC);

CREATE TABLE run_step (
    run_id      uuid NOT NULL REFERENCES run (id) ON DELETE CASCADE,
    node_id     text NOT NULL,
    path        text NOT NULL,
    alias       text,
    status      text NOT NULL CHECK (status IN ('pending', 'running', 'ok', 'failed', 'skipped')),
    error       text,
    started_at  timestamptz,
    finished_at timestamptz,
    PRIMARY KEY (run_id, node_id)
);
COMMENT ON TABLE run_step IS '节点级执行状态：按 nodeId 回填，WS 推给前端时间线';

COMMIT;
