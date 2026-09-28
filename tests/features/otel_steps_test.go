package features

import (
	"fmt"
	"strings"
	"time"

	"github.com/cucumber/godog"

	"github.com/eval-hub/eval-hub/internal/otel/oteltest"
)

// jobIDAttribute is the OTLP log-record attribute the adapter sets to the
// evaluation job id; used to scope a match to the current scenario's job.
const jobIDAttribute = "evalhub.job_id"

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

	// The collector accumulates records for the whole suite, so scope the match
	// to the current job's id (from the most recent create). Otherwise a record
	// with the same body exported by an earlier scenario could be selected,
	// making the follow-up evalhub.job_id assertion fail.
	jobID := tc.lastId
	if jobID == "" {
		jobID = tc.values["id"]
	}
	if jobID == "" {
		return tc.logError(fmt.Errorf("cannot match an exported log record without the current evaluation job id"))
	}

	deadline := time.Now().Add(otelLogWaitTimeout)
	for {
		records := collector.LogRecords()
		record := oteltest.FindLogRecordByBodyAndAttribute(records, substr, jobIDAttribute, jobID)
		if record != nil {
			tc.matchedLogRecord = record
			return nil
		}
		if !time.Now().Before(deadline) {
			break
		}
		time.Sleep(otelLogWaitInterval)
	}
	return tc.logError(fmt.Errorf("timed out after %v waiting for an exported log record containing %q with %s=%q", otelLogWaitTimeout, substr, jobIDAttribute, jobID))
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
	if traceID := tc.matchedLogRecord.GetTraceId(); !isValidID(traceID, traceIDLen) {
		return tc.logError(fmt.Errorf("log record has an invalid trace id %x (want a nonzero %d-byte id for trace/span correlation)", traceID, traceIDLen))
	}
	if spanID := tc.matchedLogRecord.GetSpanId(); !isValidID(spanID, spanIDLen) {
		return tc.logError(fmt.Errorf("log record has an invalid span id %x (want a nonzero %d-byte id for trace/span correlation)", spanID, spanIDLen))
	}
	return nil
}

// OTLP encodes a trace id as 16 bytes and a span id as 8 bytes; any other
// length is invalid per the OTLP log-record definition.
const (
	traceIDLen = 16
	spanIDLen  = 8
)

// isValidID reports whether b is exactly length bytes and not all zeros (an
// unset OTLP trace/span id is encoded as all zeros).
func isValidID(b []byte, length int) bool {
	if len(b) != length {
		return false
	}
	for _, v := range b {
		if v != 0 {
			return true
		}
	}
	return false
}
