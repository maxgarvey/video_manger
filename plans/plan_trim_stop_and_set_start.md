# Plan: Stop Playback and Set Start Time When Trim Clicked While Playing

## Objective

When the trim button is clicked while the video is playing, pause the video and set the start time to the current position, highlighting the region from current spot to end for easy deletion of the beginning.

## Current State

Trim button loads panel with default start=0, video continues playing.

## Proposed Change

Modify the hx-on::htmx:after-request on the trim button to also check if video is playing, pause it, and set start input to currentTime.

## Implementation Steps

1. Update the hx-on script in templates/player.html to include pausing and setting start if playing.
2. No backend changes.
3. Test by playing video, clicking trim, verify video pauses and start is set to current time.

## Testing

- Play a video.
- Click Trim while playing.
- Verify video pauses and start input is set to current time.

## Expected Outcome

Convenient way to trim off the beginning by selecting from current position to end.
