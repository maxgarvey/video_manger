# Plan: yt-dlp Import (Feature 12)

## Behaviour Change

From the library sidebar (or a config panel section), users can paste
a URL and download it into a chosen registered directory using yt-dlp.
The downloaded video is automatically added to the library.

## Implementation

### Routes
```
POST /ytdlp/download   body: url=<url>&dir_id=<id>
GET  /ytdlp/status/{job}  — poll for download status
```

For simplicity, run yt-dlp synchronously (with a long timeout) and
return the result inline. Async jobs would require more infrastructure.

Instead: POST blocks until yt-dlp finishes, returns the updated video
list or an error. The timeout is generous (10 minutes).

### Handler
1. Parse `url` and `dir_id`.
2. Look up the directory path from `dir_id`.
3. Run: `yt-dlp --no-playlist -o "%(title)s.%(ext)s" <url>` in the
   directory.
4. Sync the directory (`syncDir`) to pick up the new file.
5. Return the updated directory list (or video list).

### UI
Add a "Download from URL" section in the library sidebar, below
the Directories section. It shows:
- URL input
- Directory selector (dropdown of registered dirs)
- Download button (shows loading state via htmx)

## Tests
- `TestHandleYTDLP_MissingURL` — 400 if no URL.
- `TestHandleYTDLP_MissingDir` — 400 if no dir_id.
- `TestHandleYTDLP_NotInstalled` — graceful 500 if yt-dlp not in PATH.
- `TestHandleYTDLP_InvalidDir` — 404 if dir not in DB.
