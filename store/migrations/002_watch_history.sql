-- Watch history: tracks the last playback position and timestamp per video.
-- UNIQUE(video_id) means INSERT OR REPLACE keeps only the most recent watch.

CREATE TABLE IF NOT EXISTS watch_history (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    video_id   INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    position   REAL    NOT NULL DEFAULT 0,
    watched_at TEXT    NOT NULL DEFAULT (datetime('now')),
    UNIQUE(video_id)
);
