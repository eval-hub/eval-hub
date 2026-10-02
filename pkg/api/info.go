package api

// InfoResponse contains build metadata and tenant-specific scheduling options.
type InfoResponse struct {
	Version   string      `json:"version"`
	Build     string      `json:"build"`
	BuildDate string      `json:"build_date"`
	GitHash   string      `json:"git_hash"`
	Queues    []QueueInfo `json:"queues"`
}

// QueueInfo describes a Kueue LocalQueue available in a tenant namespace.
type QueueInfo struct {
	Name    string `json:"name"`
	Active  bool   `json:"active"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}
