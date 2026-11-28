-- name: GetState :one
SELECT item_id, capability_key, value, source, updated_at
FROM capability_state
WHERE item_id = $1 AND capability_key = $2;

-- name: ListStatesByItem :many
SELECT item_id, capability_key, value, source, updated_at
FROM capability_state
WHERE item_id = $1
ORDER BY capability_key;

-- name: ListAllStates :many
SELECT item_id, capability_key, value, source, updated_at
FROM capability_state
ORDER BY item_id, capability_key;


-- name: UpsertState :exec
INSERT INTO capability_state (item_id, capability_key, value, source, updated_at)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (item_id, capability_key) DO UPDATE
SET value = EXCLUDED.value,
    source = EXCLUDED.source,
    updated_at = EXCLUDED.updated_at;

-- name: InsertHistory :exec
INSERT INTO state_history (item_id, capability_key, value, source, recorded_at)
VALUES ($1, $2, $3, $4, $5);

-- name: ListStateHistory :many
SELECT id, item_id, capability_key, value, source, recorded_at
FROM state_history
WHERE item_id = $1
  AND (sqlc.narg('capability_key')::text IS NULL OR capability_key = sqlc.narg('capability_key'))
ORDER BY recorded_at DESC, id DESC
LIMIT $2;

-- name: CleanupOldStateHistory :execrows
DELETE FROM state_history WHERE recorded_at < now() - interval '30 days';

-- name: CleanupOldCommandLogs :execrows
DELETE FROM command_log WHERE created_at < now() - interval '90 days';

-- name: InsertCommand :one
INSERT INTO command_log (request_id, item_id, capability_key, target, status, source, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id;

-- name: CompleteCommand :exec
UPDATE command_log
SET status = $2, error = $3, error_code = $4, completed_at = $5, duration_ms = $6
WHERE request_id = $1;

-- name: GetCommandByRequestID :one
SELECT id, request_id, item_id, capability_key, target, status, error, error_code, source, created_by,
       created_at, completed_at, duration_ms
FROM command_log
WHERE request_id = $1;

-- name: ListCommandLogs :many
SELECT id, request_id, item_id, capability_key, target, status, error, error_code, source, created_by,
       created_at, completed_at, duration_ms
FROM command_log
ORDER BY created_at DESC, id DESC
LIMIT $1;

-- name: ListCorners :many
SELECT id, x, y
FROM corner
ORDER BY created_at, id;

-- name: UpsertCorner :one
-- 端点按坐标去重：已存在就复用（只为了拿回 id，不做实质更新）。
INSERT INTO corner (x, y)
VALUES ($1, $2)
ON CONFLICT (x, y) DO UPDATE SET x = EXCLUDED.x
RETURNING id;

-- name: ListWalls :many
SELECT w.id, w.a, w.b, w.thickness, w.arc_extent,
       ca.x AS ax, ca.y AS ay, cb.x AS bx, cb.y AS by
FROM wall w
JOIN corner ca ON ca.id = w.a
JOIN corner cb ON cb.id = w.b
ORDER BY w.created_at, w.id;

-- name: GetWallByPairAndArc :one
-- 反着画也是同一面墙；弧墙与直墙可以共用一对端点（半圆阳台），
-- 所以弧度和端点一起构成身份。
SELECT id
FROM wall
WHERE ((a = $1 AND b = $2) OR (a = $2 AND b = $1))
  AND arc_extent = sqlc.arg(arc_extent)::double precision;

-- name: InsertWall :one
INSERT INTO wall (a, b, thickness, arc_extent)
VALUES ($1, $2, $3, $4)
RETURNING id;

-- name: DeleteWall :execrows
DELETE FROM wall WHERE id = $1;

-- name: GetPlanVersion :one
SELECT version FROM plan_meta WHERE id = 1;

-- name: LockPlanVersion :one
-- 事务里先锁这一行，才能让「读版本 → 比对 → 写 → +1」不被并发插队。
SELECT version FROM plan_meta WHERE id = 1 FOR UPDATE;

-- name: BumpPlanVersion :one
UPDATE plan_meta SET version = version + 1, updated_at = now() WHERE id = 1
RETURNING version;

-- name: ListWallIDs :many
SELECT id FROM wall;

-- name: InsertWallWithID :exec
-- 撤销/重做要保住墙 id：门窗的 wall_id、前端的选中态都挂在它上面。
INSERT INTO wall (id, a, b, thickness, arc_extent) VALUES ($1, $2, $3, $4, $5);

-- name: UpdateWallGeom :execrows
UPDATE wall SET a = $2, b = $3, thickness = $4, arc_extent = $5 WHERE id = $1;

-- name: DeleteWallByID :exec
DELETE FROM wall WHERE id = $1;

-- name: ListRoomNames :many
-- 房间只存名字：几何由墙推导，哈希变了旧名字自然失配。
SELECT polygon_hash, name, kind
FROM room_name;

-- name: GetWall :one
SELECT a, b, thickness, arc_extent FROM wall WHERE id = $1;

-- name: UpdateWall :execrows
UPDATE wall SET a = $2, b = $3, thickness = $4, arc_extent = $5 WHERE id = $1;

-- name: DeleteOrphanCorners :exec
DELETE FROM corner c
WHERE NOT EXISTS (SELECT 1 FROM wall w WHERE w.a = c.id OR w.b = c.id);

-- name: UpsertRoomName :exec
INSERT INTO room_name (polygon_hash, name, kind)
VALUES ($1, $2, $3)
ON CONFLICT (polygon_hash) DO UPDATE
SET name = EXCLUDED.name,
    kind = EXCLUDED.kind,
    updated_at = now();

-- name: DeleteRoomName :execrows
DELETE FROM room_name WHERE polygon_hash = $1;

-- name: ListOpenings :many
-- 开口用「墙 + 沿墙偏移」定位；渲染时前端按墙角度换算到世界坐标。
SELECT o.id, o.wall_id, o.kind, o."offset", o.width, o.height, o.sill
FROM opening o
ORDER BY o.wall_id, o."offset", o.id;

-- name: GetOpening :one
SELECT o.id, o.wall_id, o.kind, o."offset", o.width, o.height, o.sill
FROM opening o
WHERE o.id = $1;

-- name: InsertOpening :one
INSERT INTO opening (wall_id, kind, "offset", width, height, sill)
VALUES ($1, $2, $3, $4, $5, $6)
RETURNING id;

-- name: UpdateOpening :execrows
UPDATE opening
SET "offset" = $2, width = $3, height = $4, sill = $5
WHERE id = $1;

-- name: DeleteOpening :execrows
DELETE FROM opening WHERE id = $1;

-- name: GetWallByID :one
-- 单个墙的端点坐标：挂墙物品与门窗换算位置时要用。
SELECT w.id, w.a, w.b, w.thickness, w.arc_extent,
       ca.x AS ax, ca.y AS ay, cb.x AS bx, cb.y AS by
FROM wall w
JOIN corner ca ON ca.id = w.a
JOIN corner cb ON cb.id = w.b
WHERE w.id = $1;

-- name: ListItemsOnWall :many
-- 改墙后要重算位置与朝向：带上物品宽度，偏移超出新墙长时才好夹住。
SELECT i.id, i.wall_offset, t.default_width
FROM item i
JOIN item_type t ON t.key = i.type_key
WHERE i.wall_id = $1
ORDER BY i.created_at, i.id;

-- name: UpdateItemPlacement :exec
UPDATE item
SET x = $2, y = $3, rotation = $4, wall_offset = $5, updated_at = now()
WHERE id = $1;

-- name: ListOpeningsOnWall :many
-- 墙改长之后开口要夹回墙内，否则门会一半悬在墙外。
SELECT id, "offset", width
FROM opening
WHERE wall_id = $1
ORDER BY "offset", id;

-- name: UpdateOpeningOffset :exec
UPDATE opening SET "offset" = $2 WHERE id = $1;
