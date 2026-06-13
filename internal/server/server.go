package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// Server wraps http.Server with explicit timeouts and graceful shutdown.
// No-timeout servers are a slow-loris risk; graceful shutdown matters in K8s
// where pods are killed often.
type Server struct {
	httpServer      *http.Server
	logger          *slog.Logger
	shutdownTimeout time.Duration
}

func New(addr string, mux *http.ServeMux, logger *slog.Logger, shutdownTimeout time.Duration) *Server {
	return &Server{
		httpServer: &http.Server{
			Addr:         addr,
			Handler:      mux,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 60 * time.Second, // LLM calls are slow
			IdleTimeout:  90 * time.Second,
		},
		logger:          logger,
		shutdownTimeout: shutdownTimeout,
	}
}

// Run serves until ctx is cancelled (SIGTERM), then drains in-flight requests.
func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("server listening", "addr", s.httpServer.Addr)
		if err := s.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		s.logger.Info("shutdown signal received, draining connections")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.shutdownTimeout)
		defer cancel()
		return s.httpServer.Shutdown(shutdownCtx)
	}
}