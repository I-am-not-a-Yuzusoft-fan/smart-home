-- name: ListModes :many
SELECT id, name, definition, run_mode, enabled, created_by, created_at, updated_at
FROM mode
ORDER BY created_at ASC, name ASC;

-- name: GetMode :one
SELECT id, name, definition, run_mode, enabled, created_by, created_at, updated_at
FROM mode
WHERE id = $1;

-- name: InsertMode :one
INSERT INTO mode (name, definition, run_mode, enabled, created_by, created_at, updated_at)
VALUES ($1, $2, $3, $4, $5, $6, $6)
RETURNING id, name, definition, run_mode, enabled, created_by, created_at, updated_at;

-- name: UpdateMode :one
UPDATE mode
SET name = $2,
    definition = $3,
    run_mode = $4,
    enabled = $5,
    updated_at = $6
WHERE id = $1
RETURNING id, name, definition, run_mode, enabled, created_by, created_at, updated_at;

-- name: DeleteMode :execrows
DELETE FROM mode
WHERE id = $1;
