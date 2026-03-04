# Plan: Improve Trim UI with Seek on Input Change

## Objective

Allow fine-tuning trim times with numerical inputs that seek the video to the entered time on change, providing immediate feedback.

## Current State

Trim inputs are text fields with no interaction with video playback.

## Proposed Change

Add oninput event to start and end inputs in trim_panel.html to seek video to the parsed time value.

For draggable bar, defer to future implementation as it requires more complex UI.

## Implementation Steps

1. Edit templates/trim_panel.html to add oninput="let vid = document.getElementById('vid-{{.Video.ID}}'); if (vid) vid.currentTime = parseFloat(this.value) || 0;" to both inputs.
2. No backend changes.
3. Test by entering times, verify video seeks.

## Testing

- Open trim panel.
- Change start time, verify video seeks to that time.
- Change end time, verify video seeks to that time.

## Expected Outcome

Fine-grained control with immediate visual feedback by seeking.
