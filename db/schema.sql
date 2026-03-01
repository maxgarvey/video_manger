CREATE TABLE IF NOT EXISTS directories (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    path TEXT    NOT NULL UNIQUE
);

-- directory_id is nullable: ON DELETE SET NULL keeps videos accessible
-- after a directory is removed from the library without deleting files.
-- directory_path is stored directly so FilePath() works even when
-- directory_id is NULL (i.e. the directory has been unlinked).
CREATE TABLE IF NOT EXISTS videos (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    filename       TEXT    NOT NULL,
    directory_id   INTEGER REFERENCES directories(id) ON DELETE SET NULL,
    directory_path TEXT    NOT NULL DEFAULT '',
    display_name   TEXT    NOT NULL DEFAULT '',
    UNIQUE(filename, directory_path)
);

CREATE TABLE IF NOT EXISTS tags (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT    NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS video_tags (
    video_id INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    tag_id   INTEGER NOT NULL REFERENCES tags(id)   ON DELETE CASCADE,
    PRIMARY KEY(video_id, tag_id)
);
