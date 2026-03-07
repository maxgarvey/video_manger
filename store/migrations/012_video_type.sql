-- add a video_type column to support Feature 6
ALTER TABLE videos ADD COLUMN video_type TEXT NOT NULL DEFAULT '';
CREATE INDEX IF NOT EXISTS idx_videos_video_type ON videos(video_type);
