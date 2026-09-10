// Package health exposes a minimal HTTP endpoint for Kubernetes liveness
// and readiness probes.
package health

import (
	"context"
	"net/http"
)

// Serve starts an HTTP server on addr with a "/healthz" endpoint, and shuts
// it down when ctx is cancelled. errs receives a fatal listen error, if any.
func Serve(ctx context.Context, addr string, errs chan<- error) {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})

	srv := &http.Server{Addr: addr, Handler: mux}

	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()

	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		errs <- err
	}
}
