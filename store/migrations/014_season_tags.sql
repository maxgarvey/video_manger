-- Convert existing season_number column values to season: system tags.
-- Season 0 means "unset" and is skipped.

INSERT OR IGNORE INTO tags (name)
  SELECT 'season:' || season_number FROM videos WHERE season_number > 0;

INSERT OR IGNORE INTO video_tags (video_id, tag_id)
  SELECT v.id, t.id FROM videos v
  JOIN tags t ON t.name = 'season:' || v.season_number
  WHERE v.season_number > 0;
