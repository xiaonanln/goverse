// Package clusterinfo provides interfaces for accessing cluster information.
// This package is designed to be imported by both node and gate packages without
// causing circular dependencies with the cluster package.
package clusterinfo

// ClusterInfoProvider is an interface that provides cluster-level information
// to components that need to know about connected nodes and registered gates.
// The cluster package implements this interface and passes it to nodes and gates
// during initialization.
type ClusterInfoProvider interface {
	// GetConnectedNodes returns the list of node addresses that this component
	// is currently connected to. For a node, this returns other nodes in the cluster.
	// For a gate, this returns the nodes the gate is connected to.
	GetConnectedNodes() []string

	// GetRegisteredGates returns the list of gate addresses that are registered
	// to this node. This is only meaningful for nodes; gates should return nil.
	GetRegisteredGates() []string
}
