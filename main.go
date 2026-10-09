// Command pe-challenge-p1-go-app serves a static greeting over HTTP.
// The listen port comes from HTTP_PORT and defaults to 8080.
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"
)

const (
	defaultPort = "8080"
	greeting    = "Hello from Mondoo Engineer!"
)

// version is overridden at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))

	if err := run(); err != nil {
		slog.Error("server failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	port, err := portFromEnv(os.Getenv("HTTP_PORT"))
	if err != nil {
		return err
	}

	srv := &http.Server{
		Addr:              net.JoinHostPort("", port),
		Handler:           newMux(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		slog.Info("starting server", "addr", srv.Addr, "version", version)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
	}

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

// portFromEnv returns the port to listen on. An empty value means the
// default; anything else must be a valid TCP port.
func portFromEnv(v string) (string, error) {
	if v == "" {
		return defaultPort, nil
	}
	p, err := strconv.Atoi(v)
	if err != nil || p < 1 || p > 65535 {
		return "", fmt.Errorf("invalid HTTP_PORT %q: must be an integer between 1 and 65535", v)
	}
	return strconv.Itoa(p), nil
}

func newMux() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		if _, err := fmt.Fprintln(w, greeting); err != nil {
			slog.Warn("write response", "err", err)
		}
	})
	return mux
}
