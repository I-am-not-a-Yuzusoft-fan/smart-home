-- name: ListItems :many
SELECT i.id, i.type_key, t.kind, i.name, i.room_hash, r.name AS room_name,
       i.x, i.y, i.rotation, i.status, i.attrs, i.wall_id, i.wall_offset
FROM item i
JOIN item_type t ON t.key = i.type_key
LEFT JOIN room_name r ON r.polygon_hash = i.room_hash
WHERE (sqlc.narg('room_hash')::text IS NULL OR i.room_hash = sqlc.narg('room_hash'))
  AND (sqlc.narg('kind')::text IS NULL OR t.kind = sqlc.narg('kind'))
  AND (sqlc.narg('q')::text IS NULL OR i.name ILIKE '%' || sqlc.narg('q') || '%')
ORDER BY i.created_at, i.id;

-- name: GetItem :one
SELECT i.id, i.type_key, t.kind, i.name, i.room_hash, r.name AS room_name,
       i.x, i.y, i.rotation, i.status, i.attrs, i.wall_id, i.wall_offset
FROM item i
JOIN item_type t ON t.key = i.type_key
LEFT JOIN room_name r ON r.polygon_hash = i.room_hash
WHERE i.id = $1;

-- name: InsertItem :one
INSERT INTO item (type_key, name, room_hash, x, y, rotation, status, attrs, wall_id, wall_offset)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, sqlc.narg('wall_id')::uuid,
        sqlc.narg('wall_offset')::double precision)
RETURNING id;

-- name: UpdateItem :execrows
-- 逐字段 PATCH：未提供的字段保持数据库当前值。
UPDATE item
SET name = CASE WHEN sqlc.arg(name_set)::boolean THEN sqlc.arg(name)::text ELSE name END,
    room_hash = CASE WHEN sqlc.arg(room_set)::boolean
                     THEN NULLIF(sqlc.arg(room_hash)::text, '') ELSE room_hash END,
    x = CASE WHEN sqlc.arg(pos_set)::boolean THEN sqlc.arg(x)::double precision ELSE x END,
    y = CASE WHEN sqlc.arg(pos_set)::boolean THEN sqlc.arg(y)::double precision ELSE y END,
    rotation = CASE WHEN sqlc.arg(pos_set)::boolean
                    THEN sqlc.arg(rotation)::double precision ELSE rotation END,
    status = CASE WHEN sqlc.arg(status_set)::boolean
                  THEN NULLIF(sqlc.arg(status)::text, '') ELSE status END,
    attrs = CASE WHEN sqlc.arg(attrs_set)::boolean
                 THEN sqlc.arg(attrs)::jsonb ELSE attrs END,
    wall_id = CASE WHEN sqlc.arg(wall_set)::boolean
                   THEN sqlc.narg('wall_id')::uuid ELSE wall_id END,
    wall_offset = CASE WHEN sqlc.arg(wall_set)::boolean
                       THEN sqlc.narg('wall_offset')::double precision ELSE wall_offset END,
    updated_at = now()
WHERE id = sqlc.arg(id);

-- name: DeleteItem :execrows
DELETE FROM item WHERE id = $1;
