package logging

import (
	"context"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"go.uber.org/zap"
	"go.uber.org/zap/exp/zapslog"
	"go.uber.org/zap/zapcore"
)

// Log level env: LOG_LEVEL=debug|info|warn|error (default: info).
const envLogLevel = "LOG_LEVEL"

type ShutdownFunc func() error

// NewLogger creates and returns a new structured logger using zap as the underlying
// logging implementation, wrapped with slog's interface. The logger is configured
// with production settings and ISO8601 time encoding for consistent log formatting.
//
// Returns:
//   - *slog.Logger: A structured logger instance that can be used throughout the application
//   - error: An error if the logger could not be initialized
func NewLogger() (*slog.Logger, ShutdownFunc, error) {
	logConfig := zap.NewProductionConfig()
	logConfig.EncoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder
	if level := parseLogLevel(os.Getenv(envLogLevel)); level != nil {
		logConfig.Level = zap.NewAtomicLevelAt(*level)
	}
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

func parseLogLevel(s string) *zapcore.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		l := zapcore.DebugLevel
		return &l
	case "info", "":
		return nil
	case "warn":
		l := zapcore.WarnLevel
		return &l
	case "error":
		l := zapcore.ErrorLevel
		return &l
	default:
		return nil
	}
}

// SkipCallersForInfo logs a message at the given level with the given args, skipping the given number of callers
// the caller is the function that called this function plus one, i.e the function that called one of the Log* functions
// the skip is the number of callers to skip
// the msg is the message to log
// the args are the arguments to add to the message
// the logger is the logger to use
// the level is the level to log at
func SkipCallersForInfo(ctx context.Context, logger *slog.Logger, level slog.Level, skip int, msg string, args ...any) {
	if !logger.Enabled(ctx, level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(skip, pcs[:])
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	r.Add(args...)
	_ = logger.Handler().Handle(ctx, r)
}

func LogRequestStarted(ctx *executioncontext.ExecutionContext) {
	SkipCallersForInfo(ctx.Ctx, ctx.Logger, slog.LevelInfo, 3, "Request started")
}

func LogRequestFailed(ctx *executioncontext.ExecutionContext, code int, errorMessage string) {
	// log the failed request, the request details and requestId have already been added to the logger
	SkipCallersForInfo(ctx.Ctx, ctx.Logger, slog.LevelInfo, 3, "Request failed", "error", errorMessage, "code", code)
}

func LogRequestSuccess(ctx *executioncontext.ExecutionContext, code int, response any) {
	// TODO: we should only log the response if we are in debug mode?
	// log the successful request, the request details and requestId have already been added to the logger
	// if response != nil {
	//	SkipCallersForInfo(ctx.Ctx, ctx.Logger, slog.LevelInfo, 3, "Request successful", "response", response)
	//} else {
	SkipCallersForInfo(ctx.Ctx, ctx.Logger, slog.LevelInfo, 3, "Request successful")
	//}
	//}
}
