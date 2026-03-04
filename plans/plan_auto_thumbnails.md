# Plan: Auto-Generate Thumbnails + Regenerate Button

## Objective

Automatically generate thumbnails for videos and provide a button to regenerate them at random positions.

## Current State

Thumbnail generation handler exists but generates at fixed 10% position. No UI button or automatic generation.

## Proposed Change

### Phase 1: Random Generation

- Modify `handleGenerateThumbnail` to accept optional `position` parameter
- Use random position (0.1 to 0.9) if not specified
- Update transcode.GenerateThumbnail to accept position parameter

### Phase 2: UI Button

- Add "Regenerate Thumbnail" button in video info panel
- Button calls POST /videos/{id}/thumbnail (random position)
- Show thumbnail image if available

### Phase 3: Automatic Generation (Optional)

- Add thumbnail generation during video scan/import
- Only if ffmpeg available and not already exists

## Implementation Steps

1. Update transcode.GenerateThumbnail to accept position parameter
2. Modify handler to use random position by default
3. Add regenerate button to player.html info panel
4. Add thumbnail display in info panel
5. Test generation and UI
6. Build and commit

## Expected Outcome

Videos get random thumbnails, users can regenerate them.
