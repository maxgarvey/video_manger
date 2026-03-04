# Plan: Add Confirmation Before Trimming

## Objective

Add a confirmation dialog after the user has entered video times (or highlighted a region) before performing the trim operation.

## Current State

The trim form submits directly when the "✂ Trim" button is clicked, with no confirmation.

## Proposed Change

Add `hx-confirm` attribute to the submit button in `templates/trim_panel.html` to show a browser confirmation dialog: "Are you sure you want to trim the video to the selected start/end times? This will create a new file and cannot be undone."

## Implementation Steps

1. Edit `templates/trim_panel.html` to add `hx-confirm` to the button.
2. No backend changes needed.
3. Test by entering times and clicking trim, verify confirmation appears.

## Testing

- Open trim panel.
- Enter start/end times.
- Click Trim button.
- Verify confirmation dialog appears.
- Cancel: nothing happens.
- Confirm: trim proceeds.

## Expected Outcome

Prevents accidental trims by requiring user confirmation.
