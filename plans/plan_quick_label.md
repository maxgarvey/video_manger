# Plan: Add Quick Label Button to Info Pane

## Objective

Add a "Quick label" button to the info panel that opens a modal for entering Title, Type (Movie/TV), Season, Episode, Genre, etc., to quickly update video metadata.

## Current State

Info panel has name editor, but no quick way to set fields like genre, season.

## Proposed Change

1. Add "Quick label" button in info panel that loads a modal with form for display name, genre, season, episode, actors, studio, channel.
2. On submit, update video name and fields.
3. Use existing handleUpdateVideoFields or create new handler.

## Implementation Steps

1. Add button in templates/player.html info panel.
2. Create new template quick_label_modal.html with form.
3. Add new handler handleQuickLabelModal and handleQuickLabelSubmit.
4. Update routes.
5. Test by opening modal, entering data, verify updates.

## Testing

- Open info panel.
- Click Quick label.
- Fill form, submit.
- Verify video name and fields updated.

## Expected Outcome

Quick way to label videos without full metadata edit.
