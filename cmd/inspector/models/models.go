package models

import "time"

type GoverseNode struct {
	ID            string    `json:"id"`
	Label         string    `json:"label"`
	X             int       `json:"x"`
	Y             int       `json:"y"`
	Width         int       `json:"width"`
	Height        int       `json:"height"`
	Color         string    `json:"color"`
	Type          string    `json:"type"`
	AdvertiseAddr string    `json:"advertise_addr"`
	RegisteredAt  time.Time `json:"registered_at"`
	ObjectCount   int       `json:"object_count"`
}

type GoverseGate struct {
	ID            string    `json:"id"`
	Label         string    `json:"label"`
	X             int       `json:"x"`
	Y             int       `json:"y"`
	Width         int       `json:"width"`
	Height        int       `json:"height"`
	Color         string    `json:"color"`
	Type          string    `json:"type"`
	AdvertiseAddr string    `json:"advertise_addr"`
	RegisteredAt  time.Time `json:"registered_at"`
}

type GoverseObject struct {
	ID            string  `json:"id"`
	Label         string  `json:"label"`
	X             int     `json:"x"`
	Y             int     `json:"y"`
	Size          float64 `json:"size"`
	Color         string  `json:"color"`
	Type          string  `json:"type"`
	GoverseNodeID string  `json:"goverse_node_id"`
	ShardID       int     `json:"shard_id"`
}
