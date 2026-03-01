# Plan: Show name in hover overlay (#50)

The hover overlay in `player.html` shows title and tag pills. Add the TV show name
from file metadata (`metadata.Meta.Show`) when available.

## Backend

`handlePlayer` already fetches the video and tags. Add a best-effort `metadata.Read`
call to extract `ShowName`. If ffprobe is unavailable or the file has no Show field,
`ShowName` is "". The player template data gains a `ShowName string` field.

## Frontend

In `player.html`, render the show name above the title in the overlay when non-empty:

```
The Office           ← ShowName (smaller, dimmer)
S03E04 The Client    ← title (bold, larger)
```
