package models

import "time"

type PulseNode struct {
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

type PulseObject struct {
	ID          string  `json:"id"`
	Label       string  `json:"label"`
	X           int     `json:"x"`
	Y           int     `json:"y"`
	Size        float64 `json:"size"`
	Color       string  `json:"color"`
	Type        string  `json:"type"`
	PulseNodeID string  `json:"pulse_node_id"`
}
