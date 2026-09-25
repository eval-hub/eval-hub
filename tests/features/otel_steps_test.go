package features

import (
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/eval-hub/eval-hub/internal/otel/oteltest"
)

// otelLogWaitTimeout bounds how long a step waits for adapter log records to
// arrive at the collector. The adapter flushes its OTLP log exporter on process
// exit (atexit), which happens shortly after the job reaches a terminal state,
// so records can lag the "completed" status by a moment.
const (
	otelLogWaitTimeout  = 60 * time.Second
	otelLogWaitInterval = 500 * time.Millisecond
)

// InitializeOTELSteps registers steps that assert adapter log records were
// exported to the in-process OTLP collector (see GRPCLogsCollector).
func InitializeOTELSteps(ctx *godog.ScenarioContext, tc *scenarioConfig) {
	ctx.Step(`^the OTEL collector should have received a log record containing "([^"]*)"$`, tc.theOTELCollectorShouldHaveReceivedLogContaining)
	ctx.Step(`^the log record should have attribute "([^"]*)" with value "([^"]*)"$`, tc.theLogRecordShouldHaveAttributeWithValue)
	ctx.Step(`^the log record should have attribute "([^"]*)"$`, tc.theLogRecordShouldHaveAttribute)
	ctx.Step(`^the log record severity should be "([^"]*)"$`, tc.theLogRecordSeverityShouldBe)
	ctx.Step(`^the log record should have a valid trace id and span id$`, tc.theLogRecordShouldHaveTraceAndSpanID)
}

func (tc *scenarioConfig) collector() (*oteltest.GRPCLogsCollector, error) {
	if tc.apiFeature == nil || tc.apiFeature.otelLogsCollector == nil {
		return nil, tc.logError(fmt.Errorf("OTLP logs collector is not available; OTEL log assertions require the embedded local-runtime server"))
	}
	return tc.apiFeature.otelLogsCollector, nil
}

func (tc *scenarioConfig) theOTELCollectorShouldHaveReceivedLogContaining(substr string) error {
	collector, err := tc.collector()
	if err != nil {
		return err
	}

	deadline := time.Now().Add(otelLogWaitTimeout)
	for {
		if record := oteltest.FindLogRecordByBody(collector.LogRecords(), substr); record != nil {
			tc.matchedLogRecord = record
			return nil
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(otelLogWaitInterval)
	}
	return tc.logError(fmt.Errorf("timed out after %v waiting for an exported log record containing %q", otelLogWaitTimeout, substr))
}

func (tc *scenarioConfig) requireMatchedLogRecord() error {
	if tc.matchedLogRecord == nil {
		return tc.logError(fmt.Errorf("no log record matched; run the \"received a log record containing\" step first"))
	}
	return nil
}

func (tc *scenarioConfig) theLogRecordShouldHaveAttributeWithValue(key, expected string) error {
	if err := tc.requireMatchedLogRecord(); err != nil {
		return err
	}
	resolved, err := tc.getValue(expected)
	if err != nil {
		return err
	}
	got, ok := oteltest.LogRecordAttribute(tc.matchedLogRecord, key)
	if !ok {
		return tc.logError(fmt.Errorf("log record is missing attribute %q", key))
	}
	if got != resolved {
		return tc.logError(fmt.Errorf("log record attribute %q = %q, want %q", key, got, resolved))
	}
	return nil
}

func (tc *scenarioConfig) theLogRecordShouldHaveAttribute(key string) error {
	if err := tc.requireMatchedLogRecord(); err != nil {
		return err
	}
	if _, ok := oteltest.LogRecordAttribute(tc.matchedLogRecord, key); !ok {
		return tc.logError(fmt.Errorf("log record is missing attribute %q", key))
	}
	return nil
}

func (tc *scenarioConfig) theLogRecordSeverityShouldBe(expected string) error {
	if err := tc.requireMatchedLogRecord(); err != nil {
		return err
	}
	got := tc.matchedLogRecord.GetSeverityText()
	if !strings.EqualFold(got, expected) {
		return tc.logError(fmt.Errorf("log record severity = %q, want %q", got, expected))
	}
	return nil
}

func (tc *scenarioConfig) theLogRecordShouldHaveTraceAndSpanID() error {
	if err := tc.requireMatchedLogRecord(); err != nil {
		return err
	}
	if !isNonZero(tc.matchedLogRecord.GetTraceId()) {
		return tc.logError(fmt.Errorf("log record has no trace id (expected trace/span correlation)"))
	}
	if !isNonZero(tc.matchedLogRecord.GetSpanId()) {
		return tc.logError(fmt.Errorf("log record has no span id (expected trace/span correlation)"))
	}
	return nil
}

// isNonZero reports whether b is non-empty and not all zero bytes (an
// unset OTLP trace/span id is encoded as all zeros).
func isNonZero(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	for _, v := range b {
		if v != 0 {
			return true
		}
	}
	return false
}
