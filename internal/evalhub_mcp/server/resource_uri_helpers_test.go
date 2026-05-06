package server

import (
	"net/url"
	"strings"
	"testing"

	"github.com/eval-hub/eval-hub/pkg/evalhubclient"
)

func collectPaginationParams(opts []evalhubclient.ListOption) url.Values {
	v := url.Values{}
	for _, o := range opts {
		o(v)
	}
	return v
}

func TestExtractPathID(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		rawURI  string
		kind    string
		wantID  string
		wantErr bool
	}{
		{
			name:   "success",
			rawURI: "evalhub://providers/lighteval",
			kind:   "providers",
			wantID: "lighteval",
		},
		{
			name:    "wrong host",
			rawURI:  "evalhub://benchmarks/hellaswag",
			kind:    "providers",
			wantErr: true,
		},
		{
			name:    "empty id segment",
			rawURI:  "evalhub://providers/",
			kind:    "providers",
			wantErr: true,
		},
		{
			name:    "parse error",
			rawURI:  "%",
			kind:    "providers",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			id, err := extractPathID(tt.rawURI, tt.kind)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("extractPathID() error = nil, want non-nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("extractPathID() unexpected error: %v", err)
			}
			if id != tt.wantID {
				t.Fatalf("extractPathID() id = %q, want %q", id, tt.wantID)
			}
		})
	}
}

func TestExtractLabels(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name    string
		rawURI  string
		want    []string
		wantNil bool
	}{
		{
			name:   "single label",
			rawURI: "evalhub://benchmarks?label=rag",
			want:   []string{"rag"},
		},
		{
			name:   "multiple labels",
			rawURI: "evalhub://benchmarks?label=rag&label=safety",
			want:   []string{"rag", "safety"},
		},
		{
			name:    "invalid uri",
			rawURI:  "%",
			wantNil: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := extractLabels(tt.rawURI, discardLogger)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("extractLabels() = %v, want nil", got)
				}
				return
			}
			if len(got) != len(tt.want) {
				t.Fatalf("extractLabels() len = %d, want %d", len(got), len(tt.want))
			}
			for i := range tt.want {
				if got[i] != tt.want[i] {
					t.Fatalf("extractLabels()[%d] = %q, want %q", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestExtractPagination(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name      string
		rawURI    string
		wantLimit string
		wantOff   string
		wantErr   string
	}{
		{
			name:      "no query",
			rawURI:    "evalhub://collections",
			wantLimit: "",
			wantOff:   "",
		},
		{
			name:      "limit and offset",
			rawURI:    "evalhub://collections?limit=10&offset=20",
			wantLimit: "10",
			wantOff:   "20",
		},
		{
			name:      "invalid percent uri returns empty opts",
			rawURI:    "%",
			wantLimit: "",
			wantOff:   "",
		},
		{
			name:    "invalid limit",
			rawURI:  "evalhub://jobs?limit=abc",
			wantErr: "invalid limit",
		},
		{
			name:    "invalid offset",
			rawURI:  "evalhub://jobs?offset=-",
			wantErr: "invalid offset",
		},
		{
			name:      "limit zero ignored",
			rawURI:    "evalhub://jobs?limit=0&offset=5",
			wantLimit: "",
			wantOff:   "5",
		},
		{
			name:      "negative offset ignored",
			rawURI:    "evalhub://jobs?limit=3&offset=-1",
			wantLimit: "3",
			wantOff:   "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			opts, err := extractPagination(tt.rawURI)
			if tt.wantErr != "" {
				if err == nil {
					t.Fatalf("extractPagination() err = nil, want error containing %q", tt.wantErr)
				}
				if !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("extractPagination() err = %v, want substring %q", err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatalf("extractPagination() unexpected error: %v", err)
			}
			v := collectPaginationParams(opts)
			if g, w := v.Get("limit"), tt.wantLimit; g != w {
				t.Fatalf("limit param = %q, want %q", g, w)
			}
			if g, w := v.Get("offset"), tt.wantOff; g != w {
				t.Fatalf("offset param = %q, want %q", g, w)
			}
		})
	}
}

func TestEmptyResult(t *testing.T) {
	t.Parallel()
	r := emptyResult()
	if r == nil {
		t.Fatal("emptyResult() returned nil")
	}
	if len(r.Completion.Values) != 0 {
		t.Fatalf("emptyResult().Completion.Values = %#v, want empty slice", r.Completion.Values)
	}
}
