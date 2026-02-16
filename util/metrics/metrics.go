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

	// MethodCallsTotal tracks the total number of method calls with labels for node, object_type, method_name, and status
	MethodCallsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "goverse_method_calls_total",
			Help: "Total number of method calls on distributed objects",
		},
		[]string{"node", "object_type", "method_name", "status"},
	)

	// MethodCallDuration tracks the duration of method calls in seconds
	MethodCallDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "goverse_method_call_duration",
			Help:    "Duration of method calls on distributed objects in seconds",
			Buckets: []float64{0.001, 0.01, 0.1, 1, 10},
		},
		[]string{"node", "object_type", "method_name", "status"},
	)

	// AssignedShardsTotal tracks the total number of shards assigned to each node
	AssignedShardsTotal = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "goverse_shards_total",
			Help: "Total number of shards assigned to each node",
		},
		[]string{"node"},
	)

	// ClientsConnected tracks the number of active client connections
	ClientsConnected = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "goverse_clients_connected",
			Help: "Number of active client connections in the cluster",
		},
		[]string{"node", "client_type"},
	)

	// ShardClaimsTotal tracks the total number of shard ownership claims by node
	ShardClaimsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "goverse_shard_claims_total",
			Help: "Total number of shard ownership claims by node",
		},
		[]string{"node"},
	)

	// ShardReleasesTotal tracks the total number of shard ownership releases by node
	ShardReleasesTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "goverse_shard_releases_total",
			Help: "Total number of shard ownership releases by node",
		},
		[]string{"node"},
	)

	// ShardMigrationsTotal tracks the total number of completed shard migrations
	ShardMigrationsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "goverse_shard_migrations_total",
			Help: "Total number of completed shard migrations (ownership transfers between nodes)",
		},
		[]string{"from_node", "to_node"},
	)

	// ShardsMigrating tracks the number of shards currently in migration state
	ShardsMigrating = promauto.NewGauge(
		prometheus.GaugeOpts{
			Name: "goverse_shards_migrating",
			Help: "Number of shards currently in migration state (TargetNode != CurrentNode)",
		},
	)

	// GateActiveClients tracks the number of active clients connected to each gate
	GateActiveClients = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "goverse_gate_active_clients",
			Help: "Number of active clients connected to the gate",
		},
		[]string{"gate"},
	)

	// GatePushedMessages tracks the total number of messages pushed from nodes to clients via gates
	GatePushedMessages = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "goverse_gate_pushed_messages_total",
			Help: "Total number of messages pushed to clients via this gate",
		},
		[]string{"gate"},
	)

	// GateDroppedMessages tracks the total number of messages dropped by gates
	GateDroppedMessages = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "goverse_gate_dropped_messages_total",
			Help: "Total number of messages dropped by this gate (client not found or other errors)",
		},
		[]string{"gate"},
	)

	// NodeConnectedGates tracks the number of gates connected to each node
	NodeConnectedGates = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "goverse_node_connected_gates",
			Help: "Number of gates connected to the node",
		},
		[]string{"node"},
	)

	// NodePushedMessages tracks the total number of messages pushed from nodes to gates
	NodePushedMessages = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "goverse_node_pushed_messages_total",
			Help: "Total number of messages pushed from node to gates by gate address",
		},
		[]string{"node", "gate"},
	)

	// NodeDroppedMessages tracks the total number of messages dropped by nodes
	NodeDroppedMessages = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "goverse_node_dropped_messages_total",
			Help: "Total number of messages dropped by this node (gate not connected or channel full)",
		},
		[]string{"node", "gate"},
	)

	// ShardMethodCallsTotal tracks the total number of method calls per shard
	ShardMethodCallsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "goverse_shard_method_calls_total",
			Help: "Total number of method calls per shard",
		},
		[]string{"shard"},
	)

	// OperationTimeoutsTotal tracks the total number of operation timeouts
	OperationTimeoutsTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "goverse_operation_timeouts_total",
			Help: "Total number of operation timeouts by node and operation type",
		},
		[]string{"node_addr", "operation"},
	)

	// OperationDurationSeconds tracks the duration of operations in seconds
	OperationDurationSeconds = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "goverse_operation_duration_seconds",
			Help:    "Duration of operations in seconds by node, operation type, and status",
			Buckets: []float64{0.001, 0.005, 0.01, 0.05, 0.1, 0.5, 1, 5, 10, 30},
		},
		[]string{"node_addr", "operation", "status"},
	)

	// ShardMappingWriteFailuresTotal tracks the total number of shard mapping write failures by error type
	ShardMappingWriteFailuresTotal = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Name: "goverse_shard_mapping_write_failures_total",
			Help: "Total number of shard mapping write failures by error type (modrevision_conflict, connection_error, timeout, other)",
		},
		[]string{"error_type"},
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

// RecordMethodCall increments the method call counter for a given node, object type, method name, and status
func RecordMethodCall(node, objectType, methodName, status string) {
	MethodCallsTotal.WithLabelValues(node, objectType, methodName, status).Inc()
}

// RecordMethodCallDuration records the duration of a method call in seconds
func RecordMethodCallDuration(node, objectType, methodName, status string, durationSeconds float64) {
	MethodCallDuration.WithLabelValues(node, objectType, methodName, status).Observe(durationSeconds)
}

// SetAssignedShardCount sets the total number of shards for a given node
func SetAssignedShardCount(node string, count float64) {
	AssignedShardsTotal.WithLabelValues(node).Set(count)
}

// RecordClientConnected increments the client connection count for a given node and client type
func RecordClientConnected(node, clientType string) {
	if clientType == "" {
		clientType = "grpc"
	}
	ClientsConnected.WithLabelValues(node, clientType).Inc()
}

// RecordClientDisconnected decrements the client connection count for a given node and client type
func RecordClientDisconnected(node, clientType string) {
	if clientType == "" {
		clientType = "grpc"
	}
	ClientsConnected.WithLabelValues(node, clientType).Dec()
}

// RecordShardClaim increments the shard claim counter for a given node
func RecordShardClaim(node string, count int) {
	if count > 0 {
		ShardClaimsTotal.WithLabelValues(node).Add(float64(count))
	}
}

// RecordShardRelease increments the shard release counter for a given node
func RecordShardRelease(node string, count int) {
	if count > 0 {
		ShardReleasesTotal.WithLabelValues(node).Add(float64(count))
	}
}

// RecordShardMigration increments the shard migration counter when ownership transfers between nodes
func RecordShardMigration(fromNode, toNode string) {
	if fromNode != "" && toNode != "" && fromNode != toNode {
		ShardMigrationsTotal.WithLabelValues(fromNode, toNode).Inc()
	}
}

// SetShardsMigrating sets the number of shards currently in migration state
func SetShardsMigrating(count float64) {
	ShardsMigrating.Set(count)
}

// SetGateActiveClients sets the active client count for a gate
func SetGateActiveClients(gate string, count int) {
	GateActiveClients.WithLabelValues(gate).Set(float64(count))
}

// RecordGatePushedMessage increments the pushed message counter for a gate
func RecordGatePushedMessage(gate string) {
	GatePushedMessages.WithLabelValues(gate).Inc()
}

// RecordGateDroppedMessage increments the dropped message counter for a gate
func RecordGateDroppedMessage(gate string) {
	GateDroppedMessages.WithLabelValues(gate).Inc()
}

// SetNodeConnectedGates sets the connected gates count for a node
func SetNodeConnectedGates(node string, count int) {
	NodeConnectedGates.WithLabelValues(node).Set(float64(count))
}

// RecordNodePushedMessage increments the pushed message counter for a node to a specific gate
func RecordNodePushedMessage(node, gate string) {
	NodePushedMessages.WithLabelValues(node, gate).Inc()
}

// RecordNodeDroppedMessage increments the dropped message counter for a node to a specific gate
func RecordNodeDroppedMessage(node, gate string) {
	NodeDroppedMessages.WithLabelValues(node, gate).Inc()
}

// RecordShardMethodCall increments the method call counter for a given shard
func RecordShardMethodCall(shard int) {
	ShardMethodCallsTotal.WithLabelValues(fmt.Sprintf("%d", shard)).Inc()
}

// RecordShardMappingWriteFailure increments the shard mapping write failure counter by error type
func RecordShardMappingWriteFailure(errorType string) {
	ShardMappingWriteFailuresTotal.WithLabelValues(errorType).Inc()
}

// RecordOperationTimeout increments the operation timeout counter for a given node and operation
func RecordOperationTimeout(nodeAddr, operation string) {
	OperationTimeoutsTotal.WithLabelValues(nodeAddr, operation).Inc()
}

// RecordOperationDuration records the duration of an operation in seconds
func RecordOperationDuration(nodeAddr, operation, status string, duration float64) {
	OperationDurationSeconds.WithLabelValues(nodeAddr, operation, status).Observe(duration)
}
