# Plan: mDNS .local Hostname (Feature 11)

## Behaviour Change

Advertise the server on the local network via mDNS so that other
devices can reach it at `http://video-manger.local:<port>` without
knowing the IP address.

## Library

Use `github.com/grandcat/zeroconf` — pure Go mDNS/DNS-SD library,
no CGo, well-maintained.

## Implementation

### `main.go`
On startup, after the server is configured:
1. Register an mDNS service: `_http._tcp` on the local domain,
   instance name `video-manger`, port as configured.
2. The zeroconf server runs in the background (goroutine).
3. On shutdown (Ctrl-C), deregister.

```go
server, err := zeroconf.Register("video-manger", "_http._tcp", "local.", *port, nil, nil)
if err != nil {
    log.Printf("mDNS register: %v", err)
} else {
    defer server.Shutdown()
}
```

### UI
Show `http://video-manger.local:<port>` in the Settings panel LAN
section alongside the IP addresses.

## Tests

mDNS is a network operation — test only that the helper function
returns the expected hostname string (no actual mDNS registration
in unit tests).
