package api

import "testing"

func TestNormalizeEvaluationJobConfig(t *testing.T) {
	t.Run("nil and wrong type are no-ops", func(t *testing.T) {
		NormalizeEvaluationJobConfig(nil)
		NormalizeEvaluationJobConfig("not-a-job")
	})
	t.Run("trims queue on job config", func(t *testing.T) {
		cfg := &EvaluationJobConfig{
			Queue: &QueueConfig{Name: "  user-queue  ", Kind: "kueue"},
		}
		NormalizeEvaluationJobConfig(cfg)
		if cfg.Queue.Name != "user-queue" {
			t.Fatalf("got name %q", cfg.Queue.Name)
		}
	})
}

func TestEvaluationJobConfig_NormalizeQueue(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		var cfg *EvaluationJobConfig
		cfg.Normalize()
	})
	t.Run("nil queue", func(t *testing.T) {
		cfg := &EvaluationJobConfig{}
		cfg.Normalize()
		if cfg.Queue != nil {
			t.Fatal("expected Queue to stay nil")
		}
	})
	t.Run("trims name and defaults kind", func(t *testing.T) {
		cfg := &EvaluationJobConfig{
			Queue: &QueueConfig{Name: "  user-queue  ", Kind: "  "},
		}
		cfg.Normalize()
		if cfg.Queue.Kind != "kueue" || cfg.Queue.Name != "user-queue" {
			t.Fatalf("got kind %q name %q", cfg.Queue.Kind, cfg.Queue.Name)
		}
	})
	t.Run("preserves explicit kind", func(t *testing.T) {
		cfg := &EvaluationJobConfig{
			Queue: &QueueConfig{Name: "q", Kind: "other"},
		}
		cfg.Normalize()
		if cfg.Queue.Kind != "other" {
			t.Fatalf("got kind %q", cfg.Queue.Kind)
		}
	})
}

func TestIsBenchmarkTerminalState(t *testing.T) {
	tests := []struct {
		state    State
		expected bool
	}{
		{StateCompleted, true},
		{StateFailed, true},
		{StateCancelled, true},
		{StatePending, false},
		{StateRunning, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.state), func(t *testing.T) {
			got := IsBenchmarkTerminalState(tt.state)
			if got != tt.expected {
				t.Errorf("IsBenchmarkTerminalState(%q) = %v, want %v", tt.state, got, tt.expected)
			}
		})
	}
}
