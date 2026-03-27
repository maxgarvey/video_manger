-- Partial index on unwatched videos to speed up GetNextUnwatched queries.
CREATE INDEX IF NOT EXISTS idx_videos_unwatched ON videos(watched) WHERE watched = 0;
