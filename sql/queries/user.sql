-- name: GetUserByUsername :one
SELECT id, username, display_name, password_hash, role
FROM app_user
WHERE username = $1;

-- name: GetUserByID :one
SELECT id, username, display_name, password_hash, role
FROM app_user
WHERE id = $1;
