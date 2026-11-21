-- name: ListItemTypes :many
SELECT key, kind, name, icons, default_width, default_depth,
       capabilities, capabilities_version, driver
FROM item_type
ORDER BY kind, key;

-- name: GetItemType :one
SELECT key, kind, name, icons, default_width, default_depth,
       capabilities, capabilities_version, driver
FROM item_type
WHERE key = $1;

-- name: InsertItemType :one
INSERT INTO item_type (key, kind, name, icons, default_width, default_depth,
                       capabilities, capabilities_version, driver)
VALUES ($1, $2, $3, $4, $5, $6, $7, 1, $8)
RETURNING key, kind, name, icons, default_width, default_depth,
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
    updated_at = now()
WHERE key = sqlc.arg(key)
  AND (NOT sqlc.arg(version_set)::boolean
       OR capabilities_version = sqlc.arg(expected_version)::integer)
RETURNING key, kind, name, icons, default_width, default_depth,
          capabilities, capabilities_version, driver;

-- name: DeleteItemType :execrows
DELETE FROM item_type WHERE key = $1;

-- name: CountItemsByType :one
SELECT count(*) FROM item WHERE type_key = $1;
