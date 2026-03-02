# Plan: Standardized video fields

## New columns (migration 009)
genre TEXT, season_number INTEGER, episode_number INTEGER,
episode_title TEXT, actors TEXT, studio TEXT, channel TEXT

## Store changes
- Video struct: add Genre, SeasonNumber, EpisodeNumber, EpisodeTitle,
  Actors, Studio, Channel
- Store interface: add UpdateVideoFields(ctx, id, VideoFields) error
- SQLiteStore: implement UpdateVideoFields

## Routes
GET  /videos/{id}/fields       → video_fields.html (view)
GET  /videos/{id}/fields/edit  → video_fields_edit.html (edit form)
PUT  /videos/{id}/fields       → save, return view

## UI
- In player.html info panel, add a lazy-loaded div:
  hx-get="/videos/{id}/fields" hx-trigger="load"
- video_fields.html: shows non-empty fields in a dl grid; Edit button
- video_fields_edit.html: form with all 7 inputs + Save/Cancel

## TMDB integration
- handleLookupApply: also call UpdateVideoFields with genre, season,
  episode info from the TMDB response when available.
