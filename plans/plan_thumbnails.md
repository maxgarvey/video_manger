# Plan: Support Thumbnails in the UI

## Objective

Add thumbnail support to videos with automatic generation from ffmpeg and UI display in the library and player.

## Implementation Steps

### Phase 1: Database

1. Create migration to add `thumbnail_path` field to videos table
2. Update store.Store interface with thumbnail methods
3. Regenerate sqlc code

### Phase 2: Thumbnail Generation

1. Add thumbnail generation handler that uses ffmpeg to extract frame at 10% into video
2. Save as {original_name}\_thumb.jpg in same directory as video
3. Store path in database

### Phase 3: UI Display

1. Add thumbnail display in video list with fallback to placeholder
2. Add thumbnail display in player info
3. Add "Regenerate thumbnail" button

### Phase 4: Testing

1. Run full test suite
2. Build and verify no errors
3. Commit and push

## Expected Outcome

Videos display thumbnails in UI, with ability to regenerate them.
