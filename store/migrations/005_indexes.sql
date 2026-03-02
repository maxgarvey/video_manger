-- Indexes for frequently-queried foreign keys and filter columns.
-- These columns appear in WHERE clauses on every video list render and
-- tag lookup but had no explicit indexes beyond the composite primary keys.
CREATE INDEX IF NOT EXISTS idx_videos_directory_id ON videos(directory_id);
CREATE INDEX IF NOT EXISTS idx_video_tags_tag_id   ON video_tags(tag_id);
CREATE INDEX IF NOT EXISTS idx_video_tags_video_id ON video_tags(video_id);
CREATE INDEX IF NOT EXISTS idx_watch_history_video_id ON watch_history(video_id);
CREATE INDEX IF NOT EXISTS idx_videos_rating ON videos(rating) WHERE rating > 0;
