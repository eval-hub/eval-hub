package logging

import (
	"log/slog"
	"os"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"go.uber.org/zap"
	"go.uber.org/zap/exp/zapslog"
	"go.uber.org/zap/zapcore"
)

type ShutdownFunc func() error

// NewLogger creates and returns a new structured logger using zap as the underlying
// logging implementation, wrapped with slog's interface. The logger is configured
// with production settings and ISO8601 time encoding for consistent log formatting.
//
// Returns:
//   - *slog.Logger: A structured logger instance that can be used throughout the application
//   - error: An error if the logger could not be initialized
func NewLogger() (*slog.Logger, ShutdownFunc, error) {
	var logConfig zap.Config
	logConfig = zap.NewProductionConfig()
	logConfig.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	zapLog, err := logConfig.Build()
	if err != nil {
		return nil, nil, err
	}
	f := newShutdownFunc(zapLog.Core())
	// we want the caller in our logs for debugging purposes, for now this is always set to true
	return slog.New(zapslog.NewHandler(zapLog.Core(), zapslog.WithCaller(true))), f, nil
}

func FallbackLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(os.Stdout, nil))
}

func newShutdownFunc(core zapcore.Core) ShutdownFunc {
	return func() error {
		return core.Sync()
	}
}

func LogRequestStarted(ctx *executioncontext.ExecutionContext) {
	// log the successful request, the request details and requestId have already been added to the logger
	ctx.Logger.Info("Request started")
}

func LogRequestFailed(ctx *executioncontext.ExecutionContext, code int, errorMessage string) {
	// log the failed request, the request details and requestId have already been added to the logger
	ctx.Logger.Info("Request failed", "error", errorMessage, "code", code)
}

func LogRequestSuccess(ctx *executioncontext.ExecutionContext, code int, response any) {
	// log the successful request, the request details and requestId have already been added to the logger
	if response != nil {
		ctx.Logger.Info("Request successful", "response", response)
	} else {
		ctx.Logger.Info("Request successful")
	}
}
