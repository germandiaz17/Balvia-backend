-- name: CreateCategory :one
INSERT INTO categories (
    user_id, parent_id, name, category_type, icon, color, display_order
) VALUES (
    sqlc.arg(user_id),
    sqlc.narg(parent_id),
    sqlc.arg(name),
    sqlc.arg(category_type),
    sqlc.narg(icon),
    sqlc.narg(color),
    sqlc.arg(display_order)
)
RETURNING *;

-- name: GetCategoryForUser :one
-- A category is usable if it belongs to the user or is a system category.
SELECT * FROM categories
WHERE id = $1
  AND deleted_at IS NULL
  AND (user_id = $2 OR is_system = TRUE);

-- name: GetOwnedCategory :one
-- Only the user's own (non-system) category; used for update/delete.
SELECT * FROM categories
WHERE id = $1 AND user_id = $2 AND is_system = FALSE AND deleted_at IS NULL;

-- name: ListCategoriesForUser :many
-- System categories plus the user's own, usable for selection.
SELECT * FROM categories
WHERE deleted_at IS NULL AND (user_id = $1 OR is_system = TRUE)
ORDER BY is_system DESC, category_type, display_order, name;

-- name: ListSystemCategories :many
SELECT * FROM categories
WHERE is_system = TRUE AND deleted_at IS NULL
ORDER BY category_type, display_order, name;

-- name: UpdateCategory :one
UPDATE categories
SET name = sqlc.arg(name),
    parent_id = sqlc.narg(parent_id),
    icon = sqlc.narg(icon),
    color = sqlc.narg(color),
    display_order = sqlc.arg(display_order),
    updated_at = NOW()
WHERE id = sqlc.arg(id) AND user_id = sqlc.arg(user_id) AND is_system = FALSE AND deleted_at IS NULL
RETURNING *;

-- name: SoftDeleteCategory :one
UPDATE categories
SET deleted_at = NOW(), updated_at = NOW()
WHERE id = $1 AND user_id = $2 AND is_system = FALSE AND deleted_at IS NULL
RETURNING id;
