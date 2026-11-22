-- name: ListItemTypes :many
SELECT key, kind, name, icons, default_width, default_depth, mount,
       capabilities, capabilities_version, driver
FROM item_type
ORDER BY kind, key;

-- name: GetItemType :one
SELECT key, kind, name, icons, default_width, default_depth, mount,
       capabilities, capabilities_version, driver
FROM item_type
WHERE key = $1;

-- name: InsertItemType :one
INSERT INTO item_type (key, kind, name, icons, default_width, default_depth,
                       capabilities, capabilities_version, driver, mount)
VALUES ($1, $2, $3, $4, $5, $6, $7, 1, $8, COALESCE(sqlc.narg('mount')::text, 'floor'))
RETURNING key, kind, name, icons, default_width, default_depth, mount,
          capabilities, capabilities_version, driver;

-- name: UpdateItemType :one
-- 逐字段 PATCH：未提供的字段保持数据库里的当前值，避免两个管理员并发改不同字段时互相覆盖。
-- 能力结构真变了才递增版本：PATCH 只改名字不应该让旧数据失效。
UPDATE item_type
SET name = CASE WHEN sqlc.arg(name_set)::boolean THEN sqlc.arg(name)::text ELSE name END,
    icons = CASE WHEN sqlc.arg(icons_set)::boolean
                 THEN NULLIF(sqlc.arg(icons)::text, '') ELSE icons END,
    default_width = CASE WHEN sqlc.arg(width_set)::boolean
                         THEN sqlc.arg(width)::double precision ELSE default_width END,
    default_depth = CASE WHEN sqlc.arg(depth_set)::boolean
                         THEN sqlc.arg(depth)::double precision ELSE default_depth END,
    capabilities = CASE WHEN sqlc.arg(capabilities_set)::boolean
                        THEN sqlc.arg(capabilities)::jsonb ELSE capabilities END,
    capabilities_version = capabilities_version + CASE
      WHEN sqlc.arg(capabilities_set)::boolean
       AND capabilities IS DISTINCT FROM sqlc.arg(capabilities)::jsonb
      THEN 1 ELSE 0 END,
    driver = CASE WHEN sqlc.arg(driver_set)::boolean
                  THEN NULLIF(sqlc.arg(driver)::text, '') ELSE driver END,
    mount = CASE WHEN sqlc.arg(mount_set)::boolean
                 THEN sqlc.narg('mount')::text ELSE mount END,
    updated_at = now()
WHERE key = sqlc.arg(key)
  AND (NOT sqlc.arg(version_set)::boolean
       OR capabilities_version = sqlc.arg(expected_version)::integer)
RETURNING key, kind, name, icons, default_width, default_depth, mount,
          capabilities, capabilities_version, driver;

-- name: DeleteItemType :execrows
DELETE FROM item_type WHERE key = $1;

-- name: CountItemsByType :one
SELECT count(*) FROM item WHERE type_key = $1;

-- name: CountStateRowsForKey :one
-- 某个类型下、某个能力 key 已经有多少条状态行。用来阻止「删掉的能力项换种语义加回来」。
SELECT count(*)
FROM capability_state cs
JOIN item i ON i.id = cs.item_id
WHERE i.type_key = $1
  AND cs.capability_key = $2;
