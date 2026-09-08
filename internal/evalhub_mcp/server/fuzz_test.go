package server

import (
	"context"
	"log/slog"
	"testing"
)

func FuzzExtractPathID(f *testing.F) {
	f.Add("evalhub://providers/my-provider", "providers")
	f.Add("evalhub://benchmarks/bench-1", "benchmarks")
	f.Add("evalhub://jobs/job-123", "jobs")
	f.Add("evalhub://collections/col-1", "collections")
	f.Add("evalhub://providers/", "providers")
	f.Add("evalhub://providers", "providers")
	f.Add("evalhub://wrong-kind/id", "providers")
	f.Add("", "providers")
	f.Add("://bad", "providers")
	f.Add("evalhub://providers/id/extra/segments", "providers")
	f.Add("evalhub://providers/id%20with%20spaces", "providers")

	f.Fuzz(func(t *testing.T, rawURI, kind string) {
		id, err := extractPathID(rawURI, kind)
		if err != nil {
			if id != "" {
				t.Fatalf("expected empty id on error, got %q", id)
			}
			return
		}
		if id == "" {
			t.Fatal("successful extract returned empty id")
		}
	})
}

func FuzzExtractLabels(f *testing.F) {
	f.Add("evalhub://benchmarks?label=rag&label=safety")
	f.Add("evalhub://benchmarks?label=agents")
	f.Add("evalhub://benchmarks")
	f.Add("evalhub://benchmarks?other=val")
	f.Add("")
	f.Add("://bad?label=x")

	f.Fuzz(func(t *testing.T, rawURI string) {
		labels := extractLabels(context.Background(), rawURI, slog.New(slog.DiscardHandler))
		// Must not panic; labels may be nil or populated.
		_ = labels
	})
}

func FuzzExtractStatus(f *testing.F) {
	f.Add("evalhub://jobs?status=pending")
	f.Add("evalhub://jobs?status=running")
	f.Add("evalhub://jobs?status=completed")
	f.Add("evalhub://jobs?status=failed")
	f.Add("evalhub://jobs?status=cancelled")
	f.Add("evalhub://jobs?status=partially_failed")
	f.Add("evalhub://jobs?status=invalid")
	f.Add("evalhub://jobs")
	f.Add("evalhub://jobs?status=")
	f.Add("")
	f.Add("://bad?status=pending")

	validStates := map[string]bool{
		"pending": true, "running": true, "completed": true,
		"failed": true, "cancelled": true, "partially_failed": true,
	}

	f.Fuzz(func(t *testing.T, rawURI string) {
		state, hasStatus, err := extractStatus(rawURI)
		if err != nil {
			if !hasStatus {
				t.Fatal("error returned but hasStatus is false")
			}
			return
		}
		if hasStatus && !validStates[string(state)] {
			t.Fatalf("hasStatus=true but state %q is not valid", state)
		}
	})
}

func FuzzExtractPagination(f *testing.F) {
	f.Add("evalhub://jobs?limit=10&offset=0")
	f.Add("evalhub://jobs?limit=abc")
	f.Add("evalhub://jobs?offset=-1")
	f.Add("evalhub://jobs?limit=0")
	f.Add("evalhub://jobs")
	f.Add("")
	f.Add("evalhub://jobs?limit=999999&offset=50")

	f.Fuzz(func(t *testing.T, rawURI string) {
		opts, err := extractPagination(rawURI)
		if err != nil {
			return
		}
		// Must not panic; opts may be nil or populated.
		_ = opts
	})
}
