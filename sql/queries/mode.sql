-- name: ListModes :many
SELECT id, name, definition, run_mode, enabled, created_by, created_at, updated_at
FROM mode
ORDER BY created_at ASC, name ASC;

-- name: GetMode :one
SELECT id, name, definition, run_mode, enabled, created_by, created_at, updated_at
FROM mode
WHERE id = $1;
