-- name: CreateLink :one
INSERT INTO links (url, title, content, summary, status)
VALUES (?, ?, ?, ?, ?)
RETURNING *;

-- name: GetLink :one
SELECT * FROM links
WHERE id = ?;

-- name: GetLinkByURL :one
SELECT * FROM links
WHERE url = ?;

-- name: ListLinks :many
SELECT * FROM links
ORDER BY created_at DESC
LIMIT ? OFFSET ?;

-- name: ListLinksByStatus :many
SELECT * FROM links
WHERE status = ?
ORDER BY created_at DESC
LIMIT ? OFFSET ?;

-- name: UpdateLink :one
UPDATE links
SET title = ?,
    content = ?,
    summary = ?,
    status = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;

-- name: UpdateLinkFetchedAt :exec
UPDATE links
SET fetched_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: UpdateLinkSummarizedAt :exec
UPDATE links
SET summarized_at = CURRENT_TIMESTAMP,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: DeleteLink :exec
DELETE FROM links
WHERE id = ?;

-- name: SearchLinks :many
SELECT * FROM links
WHERE 
    url LIKE ? OR
    title LIKE ? OR
    content LIKE ? OR
    summary LIKE ?
ORDER BY created_at DESC
LIMIT ? OFFSET ?;

-- name: CreateTask :one
INSERT INTO tasks (name, description)
VALUES (?, ?)
RETURNING *;

-- name: GetTask :one
SELECT * FROM tasks
WHERE id = ?;

-- name: ListTasks :many
SELECT * FROM tasks
ORDER BY created_at DESC;

-- name: ListIncompleteTasks :many
SELECT * FROM tasks
WHERE completed = 0
ORDER BY created_at DESC;

-- name: UpdateTask :one
UPDATE tasks
SET name = ?,
    description = ?,
    completed = ?,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?
RETURNING *;

-- name: CompleteTask :exec
UPDATE tasks
SET completed = 1,
    updated_at = CURRENT_TIMESTAMP
WHERE id = ?;

-- name: DeleteTask :exec
DELETE FROM tasks
WHERE id = ?;

-- name: CreateCategory :one
INSERT INTO categories (name, description)
VALUES (?, ?)
RETURNING *;

-- name: GetCategory :one
SELECT * FROM categories
WHERE id = ?;

-- name: GetCategoryByName :one
SELECT * FROM categories
WHERE name = ?;

-- name: ListCategories :many
SELECT * FROM categories
ORDER BY name;

-- name: DeleteCategory :exec
DELETE FROM categories
WHERE id = ?;

-- name: CreateTag :one
INSERT INTO tags (name)
VALUES (?)
RETURNING *;

-- name: GetTag :one
SELECT * FROM tags
WHERE id = ?;

-- name: GetTagByName :one
SELECT * FROM tags
WHERE name = ?;

-- name: ListTags :many
SELECT * FROM tags
ORDER BY name;

-- name: DeleteTag :exec
DELETE FROM tags
WHERE id = ?;

-- name: LinkTask :exec
INSERT INTO link_tasks (link_id, task_id)
VALUES (?, ?);

-- name: UnlinkTask :exec
DELETE FROM link_tasks
WHERE link_id = ? AND task_id = ?;

-- name: GetLinksForTask :many
SELECT l.* FROM links l
JOIN link_tasks lt ON l.id = lt.link_id
WHERE lt.task_id = ?
ORDER BY l.created_at DESC;

-- name: GetTasksForLink :many
SELECT t.* FROM tasks t
JOIN link_tasks lt ON t.id = lt.task_id
WHERE lt.link_id = ?
ORDER BY t.created_at DESC;

-- name: LinkCategory :exec
INSERT INTO link_categories (link_id, category_id)
VALUES (?, ?);

-- name: UnlinkCategory :exec
DELETE FROM link_categories
WHERE link_id = ? AND category_id = ?;

-- name: GetLinksForCategory :many
SELECT l.* FROM links l
JOIN link_categories lc ON l.id = lc.link_id
WHERE lc.category_id = ?
ORDER BY l.created_at DESC;

-- name: GetCategoriesForLink :many
SELECT c.* FROM categories c
JOIN link_categories lc ON c.id = lc.category_id
WHERE lc.link_id = ?
ORDER BY c.name;

-- name: LinkTag :exec
INSERT INTO link_tags (link_id, tag_id)
VALUES (?, ?);

-- name: UnlinkTag :exec
DELETE FROM link_tags
WHERE link_id = ? AND tag_id = ?;

-- name: GetLinksForTag :many
SELECT l.* FROM links l
JOIN link_tags lt ON l.id = lt.link_id
WHERE lt.tag_id = ?
ORDER BY l.created_at DESC;

-- name: GetTagsForLink :many
SELECT t.* FROM tags t
JOIN link_tags lt ON t.id = lt.tag_id
WHERE lt.link_id = ?
ORDER BY t.name;
