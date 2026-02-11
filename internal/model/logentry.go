package model

import "time"

type LogEntry struct {
	Timestamp   time.Time `json:"timestamp"`
	Pid         uint32    `json:"pid"`
	Tid         uint32    `json:"tid"`
	Fd          int32     `json:"fd"`
	CgroupID    uint64    `json:"cgroup_id"`
	Type        string    `json:"type"`
	Status      uint32    `json:"status"`
	Method      string    `json:"method"`
	Path        string    `json:"path"`
	Payload     string    `json:"payload"`
	DurationNs  uint64    `json:"duration_ns"`
	Node        string    `json:"node"`
	Namespace   string    `json:"namespace"`
	Pod         string    `json:"pod"`
	Container   string    `json:"container"`
	ContainerID string    `json:"container_id"`
}
