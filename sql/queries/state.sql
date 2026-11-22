-- name: GetState :one
SELECT item_id, capability_key, value, source, updated_at
FROM capability_state
WHERE item_id = $1 AND capability_key = $2;

-- name: ListStatesByItem :many
SELECT item_id, capability_key, value, source, updated_at
FROM capability_state
WHERE item_id = $1
ORDER BY capability_key;

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

-- name: InsertCommand :one
INSERT INTO command_log (request_id, item_id, capability_key, target, status, source, created_by)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id;

-- name: CompleteCommand :exec
UPDATE command_log
SET status = $2, error = $3, completed_at = $4, duration_ms = $5
WHERE request_id = $1;

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
SELECT w.id, w.a, w.b, w.thickness,
       ca.x AS ax, ca.y AS ay, cb.x AS bx, cb.y AS by
FROM wall w
JOIN corner ca ON ca.id = w.a
JOIN corner cb ON cb.id = w.b
ORDER BY w.created_at, w.id;

-- name: GetWallByPair :one
-- 反着画也是同一面墙，所以两种顺序都查
SELECT id
FROM wall
WHERE (a = $1 AND b = $2) OR (a = $2 AND b = $1);

-- name: InsertWall :one
INSERT INTO wall (a, b, thickness)
VALUES ($1, $2, $3)
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
INSERT INTO wall (id, a, b, thickness) VALUES ($1, $2, $3, $4);

-- name: UpdateWallGeom :execrows
UPDATE wall SET a = $2, b = $3, thickness = $4 WHERE id = $1;

-- name: DeleteWallByID :exec
DELETE FROM wall WHERE id = $1;

-- name: ListRoomNames :many
-- 房间只存名字：几何由墙推导，哈希变了旧名字自然失配。
SELECT polygon_hash, name, kind
FROM room_name;

-- name: GetWall :one
SELECT a, b FROM wall WHERE id = $1;

-- name: UpdateWall :execrows
UPDATE wall SET a = $2, b = $3, thickness = $4 WHERE id = $1;

-- name: DeleteOrphanCorners :exec
DELETE FROM corner c
WHERE NOT EXISTS (SELECT 1 FROM wall w WHERE w.a = c.id OR w.b = c.id);
