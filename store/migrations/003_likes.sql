-- Add a rating column to videos.
-- 0 = neutral, 1 = liked, 2 = double-liked (favourite).

ALTER TABLE videos ADD COLUMN rating INTEGER NOT NULL DEFAULT 0;
