# Plan: Organize Seasons and Episodes Under Shows

## Objective

Group videos by TV show/series, then by season, then by episode for better organization and navigation.

## Current State

Videos have SeasonNumber and EpisodeNumber fields but no "Show" grouping. All videos are listed flat.

## Proposed Implementation

### Phase 1: Data Model

- Add `show_name` field to videos table
- Update Video struct and store interface
- Regenerate sqlc code

### Phase 2: Show Inference

- Add logic to infer show names from:
  - Directory structure (e.g., "Breaking Bad/Season 1/")
  - Filename patterns (e.g., "Show.Name.S01E01.mp4")
  - Existing metadata fields
- Update syncDir to populate show_name during scan

### Phase 3: UI Grouping

- Modify video list template to group by show, then season
- Add collapsible sections for shows and seasons
- Update video list handler to support grouped queries

### Phase 4: Show Management

- Allow manual show name editing in quick label modal
- Add show-based filtering/navigation

## Implementation Steps

1. Add show_name to database schema and regenerate
2. Update Video struct and store methods
3. Implement show name inference logic
4. Modify video list UI for hierarchical display
5. Update handlers for grouped listings
6. Test and iterate

## Expected Outcome

Videos organized by show → season → episode structure for better TV series management.
