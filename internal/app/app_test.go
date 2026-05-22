package app

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

type closeFuncServer struct {
	called bool
	err    error
}

func (s *closeFuncServer) Close() error {
	s.called = true
	return s.err
}

func TestForceCloseAfterShutdownTimeoutClosesServerAndPreservesShutdownError(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	server := &closeFuncServer{}

	err := forceCloseAfterShutdownTimeout(logger, server, context.DeadlineExceeded)

	if !server.called {
		t.Fatal("server.Close was not called")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	logOutput := logs.String()
	if !strings.Contains(logOutput, "shutdown timeout reached; forced close starting") {
		t.Fatalf("logs = %s, want forced close starting message", logOutput)
	}
	if !strings.Contains(logOutput, "forced close completed") {
		t.Fatalf("logs = %s, want forced close completed message", logOutput)
	}
}

func TestForceCloseAfterShutdownTimeoutReturnsCloseErrorWithoutDroppingShutdownError(t *testing.T) {
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	closeErr := errors.New("close failed")
	server := &closeFuncServer{err: closeErr}

	err := forceCloseAfterShutdownTimeout(logger, server, context.DeadlineExceeded)

	if !server.called {
		t.Fatal("server.Close was not called")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want context.DeadlineExceeded", err)
	}
	if !errors.Is(err, closeErr) {
		t.Fatalf("error = %v, want close error", err)
	}
	if logOutput := logs.String(); !strings.Contains(logOutput, "forced close failed") {
		t.Fatalf("logs = %s, want forced close failed message", logOutput)
	}
}
