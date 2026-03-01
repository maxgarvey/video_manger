-- App configuration stored as key-value pairs.

CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);

-- Default values (INSERT OR IGNORE so re-applying migration is safe).
INSERT OR IGNORE INTO settings (key, value) VALUES ('autoplay_random', 'true');
INSERT OR IGNORE INTO settings (key, value) VALUES ('video_sort', 'name');
