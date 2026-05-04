package logging

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/eval-hub/eval-hub/internal/eval_hub/executioncontext"
	"go.uber.org/zap"
	"go.uber.org/zap/exp/zapslog"
	"go.uber.org/zap/zapcore"
)

// constants for the logging system
const (
	// Log level env: LOG_LEVEL=debug|info|warn|error (default: info).
	envLogLevel = "LOG_LEVEL"
)

// ShutdownFunc is a function that shuts down the logger
// the return is an error if the logger could not be shut down
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

// parseLogLevel parses the log level from the environment variable
// the s is the string to parse
// the return is the log level
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

type logLevelKeyType struct{}

// LogLevelKey is a context key for overriding the log level of a request.
var LogLevelKey = logLevelKeyType{}

// LogWithCallerSkip logs a message at the given level with the given args, skipping the given number of callers
// the caller is the function that called this function plus one, i.e the function that called one of the Log* functions
// the skip is the number of callers to skip
// the msg is the message to log
// the args are the arguments to add to the message
// the logger is the logger to use
// the level is the level to log at
func LogWithCallerSkip(ctx context.Context, logger *slog.Logger, level slog.Level, skip int, msg string, args ...any) {
	// Allow handlers to override the log level for an entire request by storing
	// a value in the context (e.g. health checks log at debug to avoid flooding
	// logs from readiness/liveness probes). This keeps the log-level decision in
	// the handler where the business logic lives, rather than in the routing layer.
	if lvl, ok := ctx.Value(LogLevelKey).(slog.Level); ok {
		level = lvl
	}
	if !logger.Enabled(ctx, level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(skip, pcs[:])
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	r.Add(args...)
	_ = logger.Handler().Handle(ctx, r)
}

func LogRequestStarted(ctx *executioncontext.ExecutionContext, args ...any) {
	LogWithCallerSkip(ctx.Ctx, ctx.Logger, slog.LevelInfo, 3, "Request started", args...)
}

func LogRequestFailed(ctx *executioncontext.ExecutionContext, code int, errorMessage string, skip ...int) {
	skipCount := 3
	if len(skip) > 0 {
		skipCount += skip[0]
	}
	// log the failed request, the request details and requestId have already been added to the logger
	LogWithCallerSkip(ctx.Ctx, ctx.Logger, slog.LevelInfo, skipCount, "Request failed", "error", errorMessage, "code", code, "duration", time.Since(ctx.StartedAt))
}

func LogRequestSuccess(ctx *executioncontext.ExecutionContext, code int, response any) {
	LogWithCallerSkip(ctx.Ctx, ctx.Logger, slog.LevelInfo, 3, "Request successful", "code", code, "duration", time.Since(ctx.StartedAt))
}

func AsPrettyJson(s any) string {
	ns, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", s)
	}
	return string(ns)
}
