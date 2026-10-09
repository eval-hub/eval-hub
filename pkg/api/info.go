package api

// InfoResponse contains build metadata and tenant-specific scheduling options.
type InfoResponse struct {
	Version          string                `json:"version"`
	Build            string                `json:"build"`
	BuildDate        string                `json:"build_date"`
	GitHash          string                `json:"git_hash"`
	HardwareProfiles []HardwareProfileInfo `json:"hardware_profiles"`
}

// QueueInfo describes a Kueue LocalQueue available in a tenant namespace.
type QueueInfo struct {
	Status  QueueAvailabilityStatus `json:"status"`
	Name    string                  `json:"name"`
	Active  bool                    `json:"active"`
	Reason  string                  `json:"reason,omitempty"`
	Message string                  `json:"message,omitempty"`
}

// HardwareProfileInfo describes an enabled platform hardware profile available to a tenant.
type HardwareProfileInfo struct {
	// QueueName is the tenant LocalQueue used by queue-backed profiles.
	PriorityClassName string                        `json:"priority_class_name,omitempty"`
	QueueName         string                        `json:"queue_name,omitempty"`
	Name              string                        `json:"name"`
	DisplayName       string                        `json:"display_name,omitempty"`
	Description       string                        `json:"description,omitempty"`
	SchedulingType    HardwareProfileSchedulingType `json:"scheduling_type,omitempty"`
	Identifiers       []HardwareProfileIdentifier   `json:"identifiers"`
	QueueAvailability *QueueAvailability            `json:"queue_availability,omitempty"`
}

// HardwareProfileSchedulingType describes how a profile schedules workloads.
type HardwareProfileSchedulingType string

const (
	HardwareProfileSchedulingNode  HardwareProfileSchedulingType = "node"
	HardwareProfileSchedulingQueue HardwareProfileSchedulingType = "queue"
)

// HardwareProfileResourceType is the resource category of a profile identifier.
type HardwareProfileResourceType string

const (
	HardwareProfileResourceCPU         HardwareProfileResourceType = "cpu"
	HardwareProfileResourceMemory      HardwareProfileResourceType = "memory"
	HardwareProfileResourceAccelerator HardwareProfileResourceType = "accelerator"
)

// HardwareProfileIdentifier describes a resource and its allowed quantities.
// Counts are strings so both integer counts and Kubernetes quantities (e.g. 2Gi)
// have a consistent representation. Unspecified counts are omitted.
type HardwareProfileIdentifier struct {
	Identifier   string                      `json:"identifier"`
	DisplayName  string                      `json:"display_name,omitempty"`
	ResourceType HardwareProfileResourceType `json:"resource_type,omitempty"`
	MinCount     string                      `json:"min_count,omitempty"`
	DefaultCount string                      `json:"default_count,omitempty"`
	MaxCount     string                      `json:"max_count,omitempty"`
}

// QueueAvailabilityStatus describes the observed state of a LocalQueue.
type QueueAvailabilityStatus string

const (
	QueueAvailabilityActive   QueueAvailabilityStatus = "active"
	QueueAvailabilityInactive QueueAvailabilityStatus = "inactive"
	QueueAvailabilityUnknown  QueueAvailabilityStatus = "unknown"
)

// QueueAvailability reports the tenant LocalQueue's Active condition.
type QueueAvailability struct {
	Status  QueueAvailabilityStatus `json:"status"`
	Reason  string                  `json:"reason,omitempty"`
	Message string                  `json:"message,omitempty"`
}
