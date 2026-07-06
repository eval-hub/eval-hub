package api

const (
	DefaultLogTailLines = 1000
	MaxLogTailLines     = 10000
)

// EvaluationLogOptions controls on-demand evaluation workload log retrieval.
type EvaluationLogOptions struct {
	TailLines    int
	Timestamps   bool
	SinceSeconds *int
}
