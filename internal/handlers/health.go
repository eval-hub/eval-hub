package handlers

import (
	"time"

	"github.com/eval-hub/eval-hub/internal/executioncontext"
	"github.com/eval-hub/eval-hub/internal/http_wrappers"
)

const (
	STATUS_HEALTHY = "healthy"
)

type HealthResponse struct {
	Status    string       `json:"status"`
	Timestamp time.Time    `json:"timestamp"`
	Storage   *StorageInfo `json:"storage,omitempty"`
}

type StorageInfo struct {
	Driver string `json:"driver"`
	URL    string `json:"url,omitempty"`
}

func (h *Handlers) HandleHealth(ctx *executioncontext.ExecutionContext, r http_wrappers.RequestWrapper, w http_wrappers.ResponseWrapper) {
	// for now we serialize on each call but we could add
	// a struct to store the health information and only
	// serialize it when something changes
	healthInfo := HealthResponse{
		Status:    STATUS_HEALTHY,
		Timestamp: time.Now().UTC(),
	}
	if h.storage != nil {
		healthInfo.Storage = &StorageInfo{
			Driver: h.storage.GetDriverName(),
			URL:    h.storage.GetConnectionURL(),
		}
	}
	w.WriteJSON(healthInfo, 200)
}
