-- name: GetUnderlay :one
SELECT content_type, width_px, height_px, sha256, cm_per_px,
       offset_x_cm, offset_y_cm, rotation_deg, opacity, visible, updated_at
FROM plan_underlay
WHERE id;

-- name: GetUnderlayImage :one
SELECT content_type, bytes
FROM plan_underlay
WHERE id;

-- name: UpsertUnderlay :exec
INSERT INTO plan_underlay (id, content_type, width_px, height_px, bytes, sha256,
                           cm_per_px, offset_x_cm, offset_y_cm, rotation_deg, opacity, visible)
VALUES (true, $1, $2, $3, $4, $5, $6, $7, $8, 0, 0.45, true)
ON CONFLICT (id) DO UPDATE
SET content_type = EXCLUDED.content_type,
    width_px     = EXCLUDED.width_px,
    height_px    = EXCLUDED.height_px,
    bytes        = EXCLUDED.bytes,
    sha256       = EXCLUDED.sha256,
    cm_per_px    = EXCLUDED.cm_per_px,
    offset_x_cm  = EXCLUDED.offset_x_cm,
    offset_y_cm  = EXCLUDED.offset_y_cm,
    rotation_deg = 0,
    opacity      = 0.45,
    visible      = true,
    updated_at   = now();

-- name: UpdateUnderlayMeta :exec
UPDATE plan_underlay
SET cm_per_px    = $1,
    offset_x_cm  = $2,
    offset_y_cm  = $3,
    rotation_deg = $4,
    opacity      = $5,
    visible      = $6,
    updated_at   = now()
WHERE id;

-- name: DeleteUnderlay :execrows
DELETE FROM plan_underlay WHERE id;
