# Plan: Optional password protection (#49)

The server has zero authentication. Anyone on the LAN can browse, delete, and
download. This plan adds an optional single-user password behind a `--password` flag.

## Design

- `--password` CLI flag. Empty (default) means no auth — existing behaviour.
- Password is bcrypt-hashed in memory at startup. Never stored to disk.
- Middleware: if a password is configured, all routes (except `GET /login`)
  require a valid session cookie. Unauthenticated requests redirect to `/login`.
- Session: random 32-byte token stored in a server-side map, set in an
  `HttpOnly; SameSite=Strict` cookie on successful login. Single session per
  server instance (no multi-user). Token has no expiry (lives until server restart).
- Login page: simple HTML form posting `POST /login` with a `password` field.
  On correct password, set cookie + redirect to `/`. On wrong password, re-render
  the form with an error.
- `GET /logout`: clears the cookie and redirects to `/login`.

## Implementation

### Dependencies
Add `golang.org/x/crypto/bcrypt` to go.mod.

### Middleware

```go
func (s *server) authMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        if s.passwordHash == nil {
            next.ServeHTTP(w, r) // no auth configured
            return
        }
        cookie, err := r.Cookie("session")
        if err != nil || !s.sessions[cookie.Value] {
            http.Redirect(w, r, "/login", http.StatusFound)
            return
        }
        next.ServeHTTP(w, r)
    })
}
```

### Session store
```go
type server struct {
    // ...existing fields...
    passwordHash []byte        // nil if no password set
    sessions     map[string]bool
    sessionsMu   sync.RWMutex
}
```

### Routes
```
GET  /login  → login form
POST /login  → verify password, set cookie, redirect
GET  /logout → clear cookie, redirect to /login
```

All other routes wrapped in authMiddleware.

### Login template
Minimal inline HTML (no layout template needed — it's a pre-auth page).

## Tests

Add handler tests:
- Unauthenticated request redirects to /login when password is set
- Correct password sets cookie and redirects
- Wrong password re-renders form with error
- Authenticated request passes through
- No-password mode: all requests pass through without auth
