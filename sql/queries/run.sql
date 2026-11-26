-- name: InsertRun :one
INSERT INTO run (id, mode_id, status, definition_snapshot, triggered_by, source, started_at)
VALUES ($1, $2, $3, $4, $5, $6, $7)
RETURNING id, mode_id, status, definition_snapshot, triggered_by, source, started_at, finished_at;

-- name: GetRun :one
SELECT id, mode_id, status, definition_snapshot, triggered_by, source, started_at, finished_at
FROM run
WHERE id = $1;

-- name: CompleteRun :exec
UPDATE run
SET status = $2, finished_at = $3
WHERE id = $1 AND status = 'running';

-- name: UpsertRunStep :one
INSERT INTO run_step (run_id, node_id, path, alias, status, error, started_at, finished_at, note)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
ON CONFLICT (run_id, node_id) DO UPDATE
SET path = EXCLUDED.path,
    alias = EXCLUDED.alias,
    status = EXCLUDED.status,
    error = EXCLUDED.error,
    started_at = COALESCE(run_step.started_at, EXCLUDED.started_at),
    finished_at = EXCLUDED.finished_at,
    note = EXCLUDED.note
RETURNING run_id, node_id, path, alias, status, error, started_at, finished_at, note;

-- name: ListRunSteps :many
SELECT run_id, node_id, path, alias, status, error, started_at, finished_at, note
FROM run_step
WHERE run_id = $1
ORDER BY path ASC, node_id ASC;
