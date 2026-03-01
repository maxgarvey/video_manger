# Plan: Filter library by liked / superliked (#55)

## Backend

- Add `ListVideosByMinRating(ctx, minRating int) ([]Video, error)` to the
  store interface and SQLiteStore — `SELECT … WHERE rating >= ?`.
- In `serveVideoList`, add `case q.Get("rating") != "":` that calls
  `ListVideosByMinRating` with the parsed rating value.

## Frontend

- In the library left panel (below "Filter by Tag"), add a "Filter by
  Rating" row with two pill buttons:
  - 👍 Liked  → `hx-vals='{"rating":"1"}'` (rating ≥ 1)
  - ⭐ Favs   → `hx-vals='{"rating":"2"}'` (rating == 2, which is
    also ≥ 2 from the min-rating query)
- Each button clears the active tag hidden field (same as the Show-all
  button) so tag and rating filters don't combine unexpectedly.
