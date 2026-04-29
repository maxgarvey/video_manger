# cmd/populate — TVMaze Metadata Tool

A one-off CLI tool for bulk-populating episode metadata from the TVMaze public API. Run directly with `go run`; not compiled into the main binary.

## Purpose

When importing a season directory of TV episodes with bare filenames (e.g. `s01e01.mkv`), this tool fetches the canonical episode list from TVMaze, matches files by season/episode number, renames them to include the episode title, and writes metadata into each file via `ffmpeg` stream-copy.

## Usage

```
go run ./cmd/populate -dir /path/to/show/season1 -show <tvmaze-show-id>
```

The TVMaze show ID is visible in the URL on [tvmaze.com](https://www.tvmaze.com) (e.g. show ID `66` for Bobs Burgers).

## Workflow

1. Read the target directory; collect video files
2. Fetch `https://api.tvmaze.com/shows/{id}/episodes` (JSON array of all episodes)
3. For each video file:
   - Parse season/episode number from filename (e.g. `s01e04` → season 1, episode 4)
   - Look up matching TVMaze episode record
   - Rename file to `S01E04 - Episode Title.ext`
   - Write metadata via `metadata.Write()`: title, air date, season/episode number, show name
4. Print summary: Renamed / Tagged / Skipped / Failed

## Output

```
Renamed: 13
Tagged:  13
Skipped: 0
Failed:  0
```

## Notes

- Files that don't match the `sNNeNN` pattern are skipped
- Files already named with the episode title are skipped (idempotent re-runs)
- No authentication required — TVMaze public API is free and unauthenticated
- This tool predates the TMDB lookup UI (`handlers_metadata.go`) which covers the same use case interactively; both approaches remain valid

## References

- Metadata write: [`metadata/`](../../metadata/OVERVIEW.md)
- Interactive alternative: TMDB lookup modal in `handlers_metadata.go`
- TVMaze API: `https://api.tvmaze.com/shows/{id}/episodes`
