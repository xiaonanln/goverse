package metrics

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	// ObjectCount tracks the total number of objects with labels for node and type
	ObjectCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "goverse_objects_total",
			Help: "Total number of distributed objects in the cluster",
		},
		[]string{"node", "type"},
	)

	// NodeConnectionCount tracks the number of connections between nodes
	NodeConnectionCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "goverse_node_connections_total",
			Help: "Total number of connections between nodes in the cluster",
		},
		[]string{"node"},
	)
)

// RecordObjectCreated increments the object count for a given node and type
func RecordObjectCreated(node, objectType string) {
	ObjectCount.WithLabelValues(node, objectType).Inc()
}

// RecordObjectDeleted decrements the object count for a given node and type
func RecordObjectDeleted(node, objectType string) {
	ObjectCount.WithLabelValues(node, objectType).Dec()
}

// SetNodeConnectionCount sets the connection count for a given node
func SetNodeConnectionCount(node string, count float64) {
	NodeConnectionCount.WithLabelValues(node).Set(count)
}
