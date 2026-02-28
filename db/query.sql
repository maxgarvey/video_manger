-- name: AddDirectory :one
INSERT INTO directories (path) VALUES (?) RETURNING *;

-- name: ListDirectories :many
SELECT * FROM directories ORDER BY path;

-- name: DeleteDirectory :exec
DELETE FROM directories WHERE id = ?;

-- name: UpsertVideo :one
INSERT INTO videos (filename, directory_id)
VALUES (?, ?)
ON CONFLICT (filename, directory_id) DO UPDATE SET filename = excluded.filename
RETURNING *;

-- name: ListVideos :many
SELECT v.id, v.filename, v.directory_id, v.display_name, d.path AS directory_path
FROM videos v
JOIN directories d ON d.id = v.directory_id
ORDER BY COALESCE(NULLIF(v.display_name, ''), v.filename);

-- name: ListVideosByTag :many
SELECT v.id, v.filename, v.directory_id, v.display_name, d.path AS directory_path
FROM videos v
JOIN directories d ON d.id = v.directory_id
JOIN video_tags vt ON v.id = vt.video_id
WHERE vt.tag_id = ?
ORDER BY COALESCE(NULLIF(v.display_name, ''), v.filename);

-- name: GetVideoByID :one
SELECT v.id, v.filename, v.directory_id, v.display_name, d.path AS directory_path
FROM videos v
JOIN directories d ON d.id = v.directory_id
WHERE v.id = ?;

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
