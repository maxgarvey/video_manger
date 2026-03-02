-- Add standardised descriptive fields to videos.
ALTER TABLE videos ADD COLUMN genre         TEXT    NOT NULL DEFAULT '';
ALTER TABLE videos ADD COLUMN season_number INTEGER NOT NULL DEFAULT 0;
ALTER TABLE videos ADD COLUMN episode_number INTEGER NOT NULL DEFAULT 0;
ALTER TABLE videos ADD COLUMN episode_title  TEXT    NOT NULL DEFAULT '';
ALTER TABLE videos ADD COLUMN actors        TEXT    NOT NULL DEFAULT '';
ALTER TABLE videos ADD COLUMN studio        TEXT    NOT NULL DEFAULT '';
ALTER TABLE videos ADD COLUMN channel       TEXT    NOT NULL DEFAULT '';
