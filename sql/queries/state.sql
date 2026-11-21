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
