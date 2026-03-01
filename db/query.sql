-- name: AddDirectory :one
INSERT INTO directories (path) VALUES (?) RETURNING *;

-- name: ListDirectories :many
SELECT * FROM directories ORDER BY path;

-- name: DeleteDirectory :exec
DELETE FROM directories WHERE id = ?;

-- Video queries are implemented as raw SQL in store/sqlite.go
-- because directory_id is nullable (orphaned videos must remain visible).

-- name: UpdateVideoName :exec
UPDATE videos SET display_name = ? WHERE id = ?;

-- name: UpsertTag :one
INSERT INTO tags (name) VALUES (?)
ON CONFLICT (name) DO UPDATE SET name = excluded.name
RETURNING *;

-- name: ListTags :many
SELECT * FROM tags ORDER BY name;

-- name: TagVideo :exec
INSERT OR IGNORE INTO video_tags (video_id, tag_id) VALUES (?, ?);

-- name: UntagVideo :exec
DELETE FROM video_tags WHERE video_id = ? AND tag_id = ?;

-- name: ListTagsByVideo :many
SELECT t.* FROM tags t
JOIN video_tags vt ON t.id = vt.tag_id
WHERE vt.video_id = ?
ORDER BY t.name;
