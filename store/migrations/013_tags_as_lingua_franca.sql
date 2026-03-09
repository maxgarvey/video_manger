-- Migrate video_type, show_name, genre, actors, studio, channel columns to system tags.
-- Actor splitting is handled by the Go migration hook (migrate_013.go).

-- Phase 1: create system tags from existing column values
INSERT OR IGNORE INTO tags (name)
  SELECT 'type:' || video_type FROM videos WHERE video_type != '';
INSERT OR IGNORE INTO tags (name)
  SELECT 'show:' || show_name FROM videos WHERE show_name != '';
INSERT OR IGNORE INTO tags (name)
  SELECT 'genre:' || genre FROM videos WHERE genre != '';
INSERT OR IGNORE INTO tags (name)
  SELECT 'studio:' || studio FROM videos WHERE studio != '';
INSERT OR IGNORE INTO tags (name)
  SELECT 'channel:' || channel FROM videos WHERE channel != '';

-- Phase 2: associate system tags with videos
INSERT OR IGNORE INTO video_tags (video_id, tag_id)
  SELECT v.id, t.id FROM videos v JOIN tags t ON t.name = 'type:' || v.video_type
  WHERE v.video_type != '';
INSERT OR IGNORE INTO video_tags (video_id, tag_id)
  SELECT v.id, t.id FROM videos v JOIN tags t ON t.name = 'show:' || v.show_name
  WHERE v.show_name != '';
INSERT OR IGNORE INTO video_tags (video_id, tag_id)
  SELECT v.id, t.id FROM videos v JOIN tags t ON t.name = 'genre:' || v.genre
  WHERE v.genre != '';
INSERT OR IGNORE INTO video_tags (video_id, tag_id)
  SELECT v.id, t.id FROM videos v JOIN tags t ON t.name = 'studio:' || v.studio
  WHERE v.studio != '';
INSERT OR IGNORE INTO video_tags (video_id, tag_id)
  SELECT v.id, t.id FROM videos v JOIN tags t ON t.name = 'channel:' || v.channel
  WHERE v.channel != '';

-- Phase 3: drop indexes that reference the columns being dropped
DROP INDEX IF EXISTS idx_videos_video_type;

-- Phase 4: drop the now-redundant columns (modernc sqlite 3.45+ supports DROP COLUMN)
ALTER TABLE videos DROP COLUMN video_type;
ALTER TABLE videos DROP COLUMN show_name;
ALTER TABLE videos DROP COLUMN genre;
ALTER TABLE videos DROP COLUMN actors;
ALTER TABLE videos DROP COLUMN studio;
ALTER TABLE videos DROP COLUMN channel;
