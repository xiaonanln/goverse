package models

import "time"

type GoverseNode struct {
	ID              string         `json:"id"`
	Label           string         `json:"label"`
	Color           string         `json:"color"`
	Type            string         `json:"type"`
	AdvertiseAddr   string         `json:"advertise_addr"`
	RegisteredAt    time.Time      `json:"registered_at"`
	ObjectCount     int            `json:"object_count"`
	ConnectedNodes  []string       `json:"connected_nodes"`  // List of other node addresses this node is connected to
	RegisteredGates []string       `json:"registered_gates"` // List of gate addresses registered to this node
	LinkMetrics     map[string]int `json:"link_metrics"`     // Map of target address -> calls per minute
}

type GoverseGate struct {
	ID             string         `json:"id"`
	Label          string         `json:"label"`
	Color          string         `json:"color"`
	Type           string         `json:"type"`
	AdvertiseAddr  string         `json:"advertise_addr"`
	RegisteredAt   time.Time      `json:"registered_at"`
	ConnectedNodes []string       `json:"connected_nodes"` // List of node addresses this gate is connected to
	Clients        int            `json:"clients"`         // Number of registered clients connected to this gate
	LinkMetrics    map[string]int `json:"link_metrics"`    // Map of target node address -> calls per minute
}

type GoverseObject struct {
	ID                     string  `json:"id"`
	Label                  string  `json:"label"`
	Size                   float64 `json:"size"`
	Color                  string  `json:"color"`
	Type                   string  `json:"type"`
	GoverseNodeID          string  `json:"goverse_node_id"`
	ShardID                int     `json:"shard_id"`
	CallsPerMinute         int     `json:"calls_per_minute"`
	AvgExecutionDurationUs float64 `json:"avg_execution_duration_us"`
}
