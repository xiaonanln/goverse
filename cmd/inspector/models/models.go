package models

import "time"

type GoverseNode struct {
	ID             string    `json:"id"`
	Label          string    `json:"label"`
	Color          string    `json:"color"`
	Type           string    `json:"type"`
	AdvertiseAddr  string    `json:"advertise_addr"`
	RegisteredAt   time.Time `json:"registered_at"`
	ObjectCount    int       `json:"object_count"`
	ConnectedNodes []string  `json:"connected_nodes"` // List of other node addresses this node is connected to
}

type GoverseGate struct {
	ID             string    `json:"id"`
	Label          string    `json:"label"`
	Color          string    `json:"color"`
	Type           string    `json:"type"`
	AdvertiseAddr  string    `json:"advertise_addr"`
	RegisteredAt   time.Time `json:"registered_at"`
	ConnectedNodes []string  `json:"connected_nodes"` // List of node addresses this gate is connected to
}

type GoverseObject struct {
	ID            string  `json:"id"`
	Label         string  `json:"label"`
	Size          float64 `json:"size"`
	Color         string  `json:"color"`
	Type          string  `json:"type"`
	GoverseNodeID string  `json:"goverse_node_id"`
	ShardID       int     `json:"shard_id"`
}
