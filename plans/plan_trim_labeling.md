# Plan: Add Clear Labeling During Trim Mode

## Objective

Add clear labeling in the trim panel to indicate that the user is selecting the region to keep, not the region to remove.

## Current State

The trim panel has a header "Trim Video" and inputs for start/end times with placeholder text. The description mentions "Accepts HH:MM:SS..." but doesn't explicitly state what the times represent.

## Proposed Change

Modify the description text in `templates/trim_panel.html` to clearly state: "Specify the start and end times of the region you want to keep. The rest will be trimmed."

This makes it unambiguous that the inputs define the kept portion.

## Implementation Steps

1. Edit `templates/trim_panel.html` to update the description paragraph.
2. No backend changes needed.
3. Test by opening the trim panel and verifying the text.

## Testing

- Open a video player.
- Click the Trim button.
- Verify the new description text appears and is clear.

## Expected Outcome

Users will understand they are selecting the region to keep, reducing confusion.
