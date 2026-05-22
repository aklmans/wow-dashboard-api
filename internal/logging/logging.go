package logging

import (
	"io"
	"log/slog"

	"github.com/aklmans/wow-dashboard-api/internal/config"
)

// NewLogger creates the process logger from typed runtime configuration.
func NewLogger(cfg *config.Config, out io.Writer) *slog.Logger {
	opts := &slog.HandlerOptions{
		Level: cfg.SlogLevel(),
	}
	if cfg.LogFormat == "json" {
		return slog.New(slog.NewJSONHandler(out, opts))
	}
	return slog.New(slog.NewTextHandler(out, opts))
}
