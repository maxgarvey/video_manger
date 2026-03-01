-- Initial schema: directories, tags, video_tags, videos.
-- Uses CREATE TABLE IF NOT EXISTS so it is safe to apply against
-- an existing database that was set up by the previous ad-hoc migration.

PRAGMA foreign_keys = ON;

CREATE TABLE IF NOT EXISTS directories (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    path TEXT    NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS tags (
    id   INTEGER PRIMARY KEY AUTOINCREMENT,
    name TEXT    NOT NULL UNIQUE
);

CREATE TABLE IF NOT EXISTS videos (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    filename       TEXT    NOT NULL,
    directory_id   INTEGER REFERENCES directories(id) ON DELETE SET NULL,
    directory_path TEXT    NOT NULL DEFAULT '',
    display_name   TEXT    NOT NULL DEFAULT '',
    UNIQUE(filename, directory_path)
);

CREATE TABLE IF NOT EXISTS video_tags (
    video_id INTEGER NOT NULL REFERENCES videos(id)   ON DELETE CASCADE,
    tag_id   INTEGER NOT NULL REFERENCES tags(id)     ON DELETE CASCADE,
    PRIMARY KEY(video_id, tag_id)
);
