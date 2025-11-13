package metrics

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ObjectCount tracks the total number of objects with labels for node, type, and shard
	ObjectCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "goverse_objects_total",
			Help: "Total number of distributed objects in the cluster",
		},
		[]string{"node", "type", "shard"},
	)

	// AssignedShardsTotal tracks the total number of shards assigned to each node
	AssignedShardsTotal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "goverse_shards_total",
			Help: "Total number of shards assigned to each node",
		},
		[]string{"node"},
	)
)

// RecordObjectCreated increments the object count for a given node, type, and shard
func RecordObjectCreated(node, objectType string, shard int) {
	ObjectCount.WithLabelValues(node, objectType, fmt.Sprintf("%d", shard)).Inc()
}

// RecordObjectDeleted decrements the object count for a given node, type, and shard
func RecordObjectDeleted(node, objectType string, shard int) {
	ObjectCount.WithLabelValues(node, objectType, fmt.Sprintf("%d", shard)).Dec()
}

// SetAssignedShardCount sets the total number of shards for a given node
func SetAssignedShardCount(node string, count float64) {
	AssignedShardsTotal.WithLabelValues(node).Set(count)
}
