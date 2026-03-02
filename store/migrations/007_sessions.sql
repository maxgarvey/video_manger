-- Persist login sessions so they survive server restarts.
-- expires_at is stored as a Unix timestamp (integer seconds).
CREATE TABLE IF NOT EXISTS sessions (
    token      TEXT    PRIMARY KEY,
    expires_at INTEGER NOT NULL
);
