-- Add show_name field to videos table for organizing TV shows
ALTER TABLE videos ADD COLUMN show_name TEXT NOT NULL DEFAULT '';
