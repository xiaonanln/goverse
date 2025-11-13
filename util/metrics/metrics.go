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
)

// RecordObjectCreated increments the object count for a given node and type
func RecordObjectCreated(node, objectType string) {
	ObjectCount.WithLabelValues(node, objectType).Inc()
}

// RecordObjectDeleted decrements the object count for a given node and type
func RecordObjectDeleted(node, objectType string) {
	ObjectCount.WithLabelValues(node, objectType).Dec()
}
