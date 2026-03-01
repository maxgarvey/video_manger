# Plan: Configuration Menu (Feature 5)

## Behaviour Change

A settings drawer accessible via a ⚙ button in the chrome (top right).
Settings are persisted in a SQLite `settings` key-value table.

## Settings to support

| Key | Type | Default | Description |
|-----|------|---------|-------------|
| `autoplay_random` | bool | true | Load random video on page open |
| `video_sort` | string | `name` | Sort order: `name`, `watched`, `rating` |

## Schema

New migration `004_settings.sql`:

```sql
CREATE TABLE IF NOT EXISTS settings (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL
);
```

## Store additions

```go
GetSetting(ctx, key string) (string, error)   // returns "" if not found
SetSetting(ctx, key, value string) error
```

## API

```
GET  /settings          — render settings form (HTML fragment)
POST /settings          — save settings, re-render form
```

## UI

- New ⚙ chrome button top-right; clicking toggles `body.config-open`.
- New `#config-panel` fixed panel (similar to info panel but from the right side or top).
- `templates/settings.html` — form with checkboxes/selects.

## Behaviour impact

- `handleRandomPlayer`: skip autoplay if `autoplay_random` == "false".
  In index.html, the `hx-get="/play/random"` trigger stays; the handler
  returns an empty placeholder when autoplay is disabled.
- `serveVideoList`: respect `video_sort` setting for ordering.
  (Currently always alphabetical — add sort options.)
