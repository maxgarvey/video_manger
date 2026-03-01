# Plan: Graceful shutdown + SIGTERM handler (#42)

Currently `log.Fatal(http.ListenAndServe(...))` means the server just exits on
SIGTERM/SIGINT without draining in-flight requests or giving the background poller a
chance to stop cleanly.

## Implementation

1. Use `signal.NotifyContext` (Go 1.16+) to create a root context that is cancelled
   on SIGINT/SIGTERM.
2. Pass that context to `startLibraryPoller` (instead of `context.Background()`), so
   the poller exits when the signal fires.
3. Wrap `http.ListenAndServe` in an `http.Server` and call `Shutdown(ctx)` after the
   signal, giving in-flight requests up to 10s to complete.
4. Close the store after shutdown.

```go
ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
defer stop()

go srv.startLibraryPoller(ctx)

httpSrv := &http.Server{Addr: ":" + *port, Handler: srv.routes()}
go func() {
    if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
        log.Printf("listen: %v", err)
    }
}()

<-ctx.Done()
stop() // release signal resources

shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
defer cancel()
if err := httpSrv.Shutdown(shutdownCtx); err != nil {
    log.Printf("shutdown: %v", err)
}
s.Close()
```

## Tests

No new test — signal handling is not easily unit-testable. The existing tests are
unaffected.
