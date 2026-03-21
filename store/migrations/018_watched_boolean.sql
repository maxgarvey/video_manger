-- Authoritative watched flag, independent of watch history
ALTER TABLE videos ADD COLUMN watched INTEGER NOT NULL DEFAULT 0;

-- Seed from existing watch_history
UPDATE videos SET watched = 1 WHERE id IN (SELECT video_id FROM watch_history);

-- History log: one row per watch cycle (cleared then rewatched = new row)
CREATE TABLE IF NOT EXISTS watch_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    video_id   INTEGER NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    watched_at TEXT    NOT NULL DEFAULT (datetime('now'))
);

-- Migrate existing watch dates into history (skip orphaned rows)
INSERT INTO watch_events (video_id, watched_at)
SELECT video_id, watched_at FROM watch_history
WHERE video_id IN (SELECT id FROM videos);
