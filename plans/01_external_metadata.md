# Plan: External Metadata Lookup (Feature 1)

## Behaviour Change

A "Look up" button in the player info panel opens a modal form where
the user can enter a movie title or TV show + season/episode to search
TMDB (The Movie Database) for metadata. Results are shown in a list;
clicking one fills the video's metadata fields.

## API Key

The user must provide a TMDB API key (v3 bearer token). It is stored
in the settings table. If no key is configured, the look-up button
shows a message directing the user to the Settings panel to add one.

## New setting

`tmdb_api_key` — stored in settings table.

Settings panel gains a text input for the key.

## Workflow

1. User clicks "🔍 Look up" in the info panel.
2. Modal (htmx-swapped into a `#lookup-modal` div) appears with:
   - Text input: "Movie or show title"
   - Optional: Season / Episode fields
   - Search button
3. POST `/videos/{id}/lookup?q=...` → hits TMDB search API →
   returns a list of results as an HTML fragment.
4. User clicks a result → POST `/videos/{id}/lookup/apply` with TMDB ID →
   fetches full details → fills metadata via `metadata.Write`.

## TMDB endpoints used

- `GET /3/search/multi?query=<q>` — search movies + TV shows.
- `GET /3/movie/{id}` — movie details.
- `GET /3/tv/{series_id}/season/{n}/episode/{n}` — episode details.

## Tests

- `TestHandleGetLookupModal_NoKey` — shows "configure API key" message.
- `TestHandleLookupSearch_NoKey` — 400 when no API key set.
- `TestHandleLookupSearch_BadRequest` — 400 for empty query.
- `TestHandleLookupApply_BadVideo` — 404 for unknown video.
