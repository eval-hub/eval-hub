package api

const (
	DefaultLogTailLines = 1000
	MaxLogTailLines     = 10000
	// AllLogLines is the sentinel value for TailLines meaning "return all available lines".
	AllLogLines = -1
)

// EvaluationLogOptions controls on-demand evaluation workload log retrieval.
type EvaluationLogOptions struct {
	TailLines    int
	Timestamps   bool
	SinceSeconds *int
}
