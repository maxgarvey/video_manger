# Plan: Like and Double-Like (Feature 7)

## Behaviour Change

Each video can be in one of three states:
- **Neutral** (default) — no rating
- **Liked** (👍) — single like
- **Double-liked** (⭐) — favourite

Clicking 👍 on a neutral video → liked.
Clicking 👍 on a liked video → neutral (toggle off).
Clicking ⭐ on any video → double-liked (or neutral if already double-liked).

## Schema

New migration: `store/migrations/003_likes.sql`

```sql
ALTER TABLE videos ADD COLUMN rating INTEGER NOT NULL DEFAULT 0;
-- 0 = neutral, 1 = liked, 2 = double-liked
```

Simple: add a `rating` column to the videos table. No separate table needed.

## Store interface additions

```go
SetVideoRating(ctx context.Context, videoID int64, rating int) error
```

`GetVideo` and `ListVideos` already return the `Video` struct — add `Rating int` field.

## API

```
POST /videos/{id}/rating   body: rating=<0|1|2>
```

Returns an updated like-button fragment (htmx swap into the player info panel).

## UI

In `player.html` info panel, show two buttons:
- 👍 Like (highlighted if rating >= 1)
- ⭐ Double-like (highlighted if rating == 2)

Clicking toggles the rating and swaps the button fragment.

In `video_list.html`, show a small indicator next to title for liked/double-liked videos.

## Store change

Add `Rating int` to `store.Video` struct and scan it in all video queries.
Add `SetVideoRating` to interface and SQLite implementation.
