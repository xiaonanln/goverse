package cluster

// NodeInfo represents information about a node
type NodeInfo struct {
	// Address is the node address (e.g., "localhost:48000")
	Address string
	// Configured indicates whether this node is configured in a config file
	Configured bool
	// IsAlive indicates whether this node is found in etcd cluster state
	IsAlive bool
	// IsLeader indicates whether this node is the cluster leader
	IsLeader bool
}

// GateInfo represents information about a gate
type GateInfo struct {
	// Address is the gate address (e.g., "localhost:49000")
	Address string
	// Configured indicates whether this gate is configured in a config file
	Configured bool
	// IsAlive indicates whether this gate is found in etcd cluster state
	IsAlive bool
}

// GetNodesInfo returns a map of node addresses to their information
// Keys are node addresses, values contain whether configured and whether found in cluster state
func (c *Cluster) GetNodesInfo() map[string]NodeInfo {
	result := make(map[string]NodeInfo)

	// Get configured nodes from config (if config was used)
	configuredNodes := c.getConfiguredNodes()
	for _, addr := range configuredNodes {
		result[addr] = NodeInfo{
			Address:    addr,
			Configured: true,
			IsAlive:    false, // Will be updated below
			IsLeader:   false, // Will be updated below
		}
	}

	// Get leader node address
	leaderAddr := c.consensusManager.GetLeaderNode()

	// Get nodes from cluster state (etcd)
	clusterStateNodes := c.consensusManager.GetNodes()
	for _, addr := range clusterStateNodes {
		isLeader := (addr == leaderAddr)
		if info, exists := result[addr]; exists {
			// Node is both configured and in cluster state
			info.IsAlive = true
			info.IsLeader = isLeader
			result[addr] = info
		} else {
			// Node is only in cluster state (not configured)
			result[addr] = NodeInfo{
				Address:    addr,
				Configured: false,
				IsAlive:    true,
				IsLeader:   isLeader,
			}
		}
	}

	return result
}

// GetGatesInfo returns a map of gate addresses to their information
// Keys are gate addresses, values contain whether configured and whether found in cluster state
func (c *Cluster) GetGatesInfo() map[string]GateInfo {
	result := make(map[string]GateInfo)

	// Get configured gates from config (if config was used)
	configuredGates := c.getConfiguredGates()
	for _, addr := range configuredGates {
		result[addr] = GateInfo{
			Address:    addr,
			Configured: true,
			IsAlive:    false, // Will be updated below
		}
	}

	// Get gates from cluster state (etcd)
	clusterStateGates := c.consensusManager.GetGates()
	for _, addr := range clusterStateGates {
		if info, exists := result[addr]; exists {
			// Gate is both configured and in cluster state
			info.IsAlive = true
			result[addr] = info
		} else {
			// Gate is only in cluster state (not configured)
			result[addr] = GateInfo{
				Address:    addr,
				Configured: false,
				IsAlive:    true,
			}
		}
	}

	return result
}

// getConfiguredNodes returns the list of configured node addresses from the config file
// Returns empty slice if no config file was used
func (c *Cluster) getConfiguredNodes() []string {
	// Check if we have a config with nodes
	if c.config.ConfigFile == nil {
		return nil
	}

	nodes := make([]string, 0, len(c.config.ConfigFile.Nodes))
	for _, node := range c.config.ConfigFile.Nodes {
		// Use advertise address if set, otherwise use grpc address
		addr := node.AdvertiseAddr
		if addr == "" {
			addr = node.GRPCAddr
		}
		nodes = append(nodes, addr)
	}
	return nodes
}

// getConfiguredGates returns the list of configured gate addresses from the config file
// Returns empty slice if no config file was used
func (c *Cluster) getConfiguredGates() []string {
	// Check if we have a config with gates
	if c.config.ConfigFile == nil {
		return nil
	}

	gates := make([]string, 0, len(c.config.ConfigFile.Gates))
	for _, gate := range c.config.ConfigFile.Gates {
		// Use advertise address if set, otherwise use grpc address
		addr := gate.AdvertiseAddr
		if addr == "" {
			addr = gate.GRPCAddr
		}
		gates = append(gates, addr)
	}
	return gates
}
