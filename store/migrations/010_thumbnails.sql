-- Add thumbnail_path field to videos table
ALTER TABLE videos ADD COLUMN thumbnail_path TEXT NOT NULL DEFAULT '';
