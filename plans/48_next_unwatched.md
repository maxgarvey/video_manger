# Plan: "Next unwatched" button (#48)

For series workflows, users want to jump to the next episode that hasn't been watched.

## Backend

New route: `GET /videos/next-unwatched`

Optional query params:
- `tag_id` — restrict to videos in a tag
- `q` — restrict to search results

Query: join videos with watch_history (LEFT JOIN), filter WHERE watch_history.video_id IS NULL,
order by filename, LIMIT 1. Return JSON `{id, title}` — same shape as `/random-video`.

## Frontend

Add a "▶ Next unwatched" button to the Tags section header in the sidebar. When
clicked, it fetches `/videos/next-unwatched?tag_id={activeTag}` and calls
`openTab(id, title)` with the result.

If no unwatched video exists, show a brief "All watched!" notification.
