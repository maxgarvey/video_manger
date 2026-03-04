# Plan: Auto-Select Front Region for Trim When Played 70%+

## Objective

When the trim button is clicked and the video has been played to 70% or more of its duration, automatically set the trim region to keep the entire front part (start=0, end=currentTime), assuming the user wants to delete the end.

## Current State

Trim button loads the panel with default start=0, end=default (end).

## Proposed Change

Add `hx-on::htmx:after-request` to the trim button in `templates/player.html` to check if progress >70%, and if so, set the end input to currentTime (floored to seconds).

## Implementation Steps

1. Edit `templates/player.html` to add the hx-on attribute to the trim button.
2. Use JS to get video element by id 'vid-{{.Video.ID}}', check progress, set input value.
3. No backend changes.
4. Test by playing a video >70%, click trim, verify end time is set to current position.

## Testing

- Play a video past 70%.
- Click Trim.
- Verify the end input is set to the current time in seconds.

## Expected Outcome

Convenient default for trimming off the end when mostly watched.
