// Inspector Graph - Objects View (Graph View)

// Objects view state
let simulation = null
let svg = null
let g = null
let zoom = null

// Helper function to apply highlighting to new objects
function applyNewObjectHighlighting(nodeSelection) {
  nodeSelection.classed('new-object', d => d.nodeType === NODE_TYPE_OBJECT && isNewObject(d.id))
}

// Helper function to calculate dynamic stroke width based on CPM
function getLinkStrokeWidth(link) {
  // Base width for different link types
  let baseWidth = 1.5
  if (link.type === 'node-node' || link.type === 'gate-node') {
    baseWidth = 2
  }
  
  // For node-node and gate-node links, add dynamic width based on CPM
  if (link.type === 'node-node' || link.type === 'gate-node') {
    const cpm = link.callsPerMinute || 0
    // Dynamic formula: Math.min(baseWidth + cpm / 10, 10)
    return Math.min(baseWidth + cpm / 10, 10)
  }
  
  return baseWidth
}

// Helper function to get link color with gradient based on CPM
function getLinkColor(link) {
  // Use base color for non-traffic links
  if (link.type !== 'node-node' && link.type !== 'gate-node') {
    return link.color || '#999'
  }
  
  const cpm = link.callsPerMinute || 0
  
  // If no traffic, use base color
  if (cpm === 0) {
    return link.color || '#999'
  }
  
  // Color gradient based on CPM
  // Low traffic (0-10 CPM): blue
  // Medium traffic (10-50 CPM): green to yellow
  // High traffic (50-100+ CPM): orange to red
  
  if (cpm < 10) {
    // Blue for low traffic
    return '#2196F3'
  } else if (cpm < 30) {
    // Green to yellow for medium-low traffic
    return '#4CAF50'
  } else if (cpm < 60) {
    // Yellow for medium traffic
    return '#FFC107'
  } else if (cpm < 100) {
    // Orange for medium-high traffic
    return '#FF9800'
  } else {
    // Red for high traffic
    return '#F44336'
  }
}

// Helper function to determine link CSS classes
function getLinkClasses(link) {
  const classes = ['graph-link']
  
  // Add active class for links with traffic
  if ((link.type === 'node-node' || link.type === 'gate-node') && link.callsPerMinute > 0) {
    classes.push('link-active')
  }
  
  return classes.join(' ')
}

// Initialize D3 graph for Objects View
function initGraph() {
  const container = document.getElementById('graph-container')
  const width = container.clientWidth
  const height = container.clientHeight

  svg = d3.select('#graph-container')
    .append('svg')
    .attr('width', width)
    .attr('height', height)

  // Add zoom behavior
  zoom = d3.zoom()
    .scaleExtent([0.1, 4])
    .on('zoom', (event) => {
      g.attr('transform', event.transform)
    })

  svg.call(zoom)

  // Add defs for arrow markers
  const defs = svg.append('defs')
  
  // Arrow marker for bidirectional links
  defs.append('marker')
    .attr('id', 'arrow')
    .attr('viewBox', '0 -5 10 10')
    .attr('refX', 8)
    .attr('refY', 0)
    .attr('markerWidth', 6)
    .attr('markerHeight', 6)
    .attr('orient', 'auto')
    .append('path')
    .attr('d', 'M0,-5L10,0L0,5')
    .attr('fill', '#666')

  // Main group for zoom/pan
  g = svg.append('g')

  // Links group
  g.append('g').attr('class', 'links')

  // Link labels group
  g.append('g').attr('class', 'link-labels')

  // Nodes group
  g.append('g').attr('class', 'nodes')

  // Labels group
  g.append('g').attr('class', 'labels')

  // Initialize force simulation
  simulation = d3.forceSimulation()
    .force('charge', d3.forceManyBody().strength(d => {
      // Gates have stronger repulsive force
      return d.nodeType === NODE_TYPE_GATE ? -5000 : -200
    }))
    .force('center', d3.forceCenter(width / 2, height / 2))
    .force('collision', d3.forceCollide().radius(d => getNodeRadius(d) + 5))
    .force('link', d3.forceLink().id(d => d.id)
      .distance(d => {
        if (d.type === 'object-shard') return 30
        if (d.type === 'object-node') return 50
        if (d.type === 'shard-node') return 80
        if (d.type === 'node-node' || d.type === 'gate-node') return 150
        return 100
      })
      .strength(d => {
        if (d.type === 'object-shard') return 2
        if (d.type === 'object-node') return 1.5
        if (d.type === 'gate-node') return 0.1
        return 1
      }))
    .on('tick', ticked)

  // Zoom controls
  document.getElementById('zoom-in').addEventListener('click', () => {
    svg.transition().duration(300).call(zoom.scaleBy, 1.3)
  })
  document.getElementById('zoom-out').addEventListener('click', () => {
    svg.transition().duration(300).call(zoom.scaleBy, 0.7)
  })
  document.getElementById('zoom-reset').addEventListener('click', () => {
    svg.transition().duration(300).call(zoom.transform, d3.zoomIdentity)
  })
}

// Handle window resize for graph view
function handleGraphResize() {
  const container = document.getElementById('graph-container')
  const newWidth = container.clientWidth
  const newHeight = container.clientHeight
  svg.attr('width', newWidth).attr('height', newHeight)
  simulation.force('center', d3.forceCenter(newWidth / 2, newHeight / 2))
  simulation.alpha(0.3).restart()
}

// Tick function for D3 force simulation
function ticked() {
  // Update node positions
  g.select('.nodes').selectAll('.graph-node')
    .attr('transform', d => `translate(${d.x}, ${d.y})`)

  // Update label positions
  g.select('.labels').selectAll('.node-label')
    .attr('x', d => d.x)
    .attr('y', d => d.y + getNodeRadius(d) + 14)

  // Update metric label positions (below object nodes)
  g.select('.labels').selectAll('.object-metric-label')
    .attr('x', d => d.x)
    .attr('y', d => d.y + getNodeRadius(d) + 24)

  // Update link positions
  g.select('.links').selectAll('.graph-link')
    .attr('x1', d => d.source.x)
    .attr('y1', d => d.source.y)
    .attr('x2', d => d.target.x)
    .attr('y2', d => d.target.y)

  // Update link label positions (centered on link)
  g.select('.link-labels').selectAll('.link-label')
    .attr('x', d => (d.source.x + d.target.x) / 2)
    .attr('y', d => (d.source.y + d.target.y) / 2)
}

// Drag handlers for graph nodes
function dragStarted(event, d) {
  if (!event.active) simulation.alphaTarget(SIMULATION_ALPHA_FULL).restart()
  d.fx = d.x
  d.fy = d.y
}

function dragged(event, d) {
  d.fx = event.x
  d.fy = event.y
}

function dragEnded(event, d) {
  if (!event.active) simulation.alphaTarget(0)
  // Release all nodes to let force simulation move them
  d.fx = null
  d.fy = null
}

// Focus on a specific node in the graph
function focusOnNode(nodeId) {
  const node = simulation.nodes().find(n => n.id === nodeId)
  if (node) {
    const container = document.getElementById('graph-container')
    const width = container.clientWidth
    const height = container.clientHeight

    svg.transition().duration(750).call(
      zoom.transform,
      d3.zoomIdentity
        .translate(width / 2, height / 2)
        .scale(1.5)
        .translate(-node.x, -node.y)
    )
  }
}

// Helper function to update node labels (for nodes and gates)
function updateNodeLabels(nodes) {
  const labelSelection = g.select('.labels')
    .selectAll('.node-label')
    .data(nodes.filter(d => d.nodeType === NODE_TYPE_NODE || d.nodeType === NODE_TYPE_GATE), d => d.id)

  labelSelection.exit().remove()

  labelSelection.enter()
    .append('text')
    .attr('class', 'node-label')
    .text(d => d.label)

  labelSelection.text(d => d.label)
}

// Helper function to update object metric labels
function updateObjectMetricLabels(nodes) {
  const metricLabelSelection = g.select('.labels')
    .selectAll('.object-metric-label')
    .data(showObjectMetricLabels ? nodes.filter(d => d.nodeType === NODE_TYPE_OBJECT) : [], d => d.id)

  metricLabelSelection.exit().remove()

  const metricLabelEnter = metricLabelSelection.enter()
    .append('text')
    .attr('class', 'object-metric-label')
    .attr('text-anchor', 'middle')
    .attr('font-size', '9px')
    .attr('fill', '#888')
    .attr('pointer-events', 'none')
    .attr('x', d => d.x)
    .attr('y', d => d.y + getNodeRadius(d) + 24)

  // Update text for both new and existing metric labels
  metricLabelEnter.merge(metricLabelSelection)
    .text(d => {
      const cpm = d.callsPerMinute || 0
      const us = Math.round(d.avgExecutionDurationUs) || 0
      return `${cpm}cpm ${us}us`
    })
}

// Helper function to update link labels
function updateLinkLabels(links) {
  const linkLabelsSelection = g.select('.link-labels')
    .selectAll('.link-label')
    .data(showLinkLabels ? links.filter(d => (d.type === 'node-node' || d.type === 'gate-node') && d.callsPerMinute > 0) : [], 
          d => `${d.source.id || d.source}-${d.target.id || d.target}`)

  linkLabelsSelection.exit().remove()

  const linkLabelsEnter = linkLabelsSelection.enter()
    .append('text')
    .attr('class', 'link-label')
    .attr('text-anchor', 'middle')
    .attr('font-size', '10px')
    .attr('font-weight', 'bold')
    .attr('fill', '#333')
    .attr('stroke', 'white')
    .attr('stroke-width', '2px')
    .attr('paint-order', 'stroke')
    .attr('pointer-events', 'none')
    .attr('x', d => (d.source.x + d.target.x) / 2)
    .attr('y', d => (d.source.y + d.target.y) / 2)

  // Update text for all link labels
  linkLabelsEnter.merge(linkLabelsSelection)
    .text(d => `${d.callsPerMinute}cpm`)
}

// Build nodes and links from graph data
function buildGraphNodesAndLinks() {
  const nodes = []
  const links = []
  const nodeMap = new Map()
  const container = document.getElementById('graph-container')
  const containerWidth = container.clientWidth

  // Add cluster nodes
  graphData.goverse_nodes.forEach(n => {
    const node = {
      id: n.id,
      label: n.label || n.id,
      nodeType: NODE_TYPE_NODE,
      advertiseAddr: n.advertise_addr,
      color: n.color,
      objectCount: n.object_count || 0,
      connectedNodes: n.connected_nodes || []
    }
    nodes.push(node)
    nodeMap.set(n.id, node)
  })

  // Add gates
  graphData.goverse_gates.forEach((g, index) => {
    const node = {
      id: g.id,
      label: g.label || g.id,
      nodeType: NODE_TYPE_GATE,
      advertiseAddr: g.advertise_addr,
      color: g.color,
      connectedNodes: g.connected_nodes || []
    }
    nodes.push(node)
    nodeMap.set(g.id, node)
  })

  // Track shards and their nodes (a shard can be on multiple nodes during migration)
  const shardToNodes = new Map() // shardId -> Set of nodeIds

  // Add objects and track their shards
  graphData.goverse_objects.forEach(obj => {
    const node = {
      id: obj.id,
      label: obj.label || obj.id,
      nodeType: NODE_TYPE_OBJECT,
      type: obj.type,
      shardId: obj.shard_id,
      goverseNodeId: obj.goverse_node_id,
      color: obj.type ? stringToColor(obj.type) : typeColors.default, // Compute color from type
      size: obj.size,
      callsPerMinute: obj.calls_per_minute,
      avgExecutionDurationUs: obj.avg_execution_duration_us
    }
    nodes.push(node)
    nodeMap.set(obj.id, node)

    // Track which nodes have objects for each shard (skip fixed-node objects with shard_id = -1)
    if (obj.shard_id !== undefined && obj.shard_id !== -1 && obj.goverse_node_id) {
      if (!shardToNodes.has(obj.shard_id)) {
        shardToNodes.set(obj.shard_id, new Set())
      }
      shardToNodes.get(obj.shard_id).add(obj.goverse_node_id)
    }
  })

  // Create shard nodes and object-to-shard links
  const shardNodeMap = new Map()
  graphData.goverse_objects.forEach(obj => {
    // Fixed-node objects (shard_id = -1) should link directly to their node
    if (obj.shard_id === -1 && obj.goverse_node_id && nodeMap.has(obj.goverse_node_id)) {
      links.push({
        source: obj.id,
        target: obj.goverse_node_id,
        type: 'object-node',
        color: '#00BCD4' // Cyan for fixed-node object-to-node
      })
    } else if (obj.shard_id !== undefined && obj.shard_id !== -1) {
      const shardId = `shard-${obj.shard_id}`
      
      // Create shard node if not exists
      if (!shardNodeMap.has(shardId)) {
        const shardNode = {
          id: shardId,
          label: `#${obj.shard_id}`,
          nodeType: NODE_TYPE_SHARD,
          shardId: obj.shard_id
        }
        nodes.push(shardNode)
        nodeMap.set(shardId, shardNode)
        shardNodeMap.set(shardId, shardNode)
      }

      // Link object to shard
      links.push({
        source: obj.id,
        target: shardId,
        type: 'object-shard',
        color: '#9C27B0' // Purple for object-to-shard
      })
    }
  })

  // Create shard-to-node links
  shardToNodes.forEach((nodeIds, shardId) => {
    const shardNodeId = `shard-${shardId}`
    const isMultiNode = nodeIds.size > 1 // Shard split across multiple nodes (error state)
    
    nodeIds.forEach(nodeId => {
      if (nodeMap.has(nodeId)) {
        links.push({
          source: shardNodeId,
          target: nodeId,
          type: 'shard-node',
          color: isMultiNode ? '#F44336' : '#FFA726' // Red if split, orange otherwise
        })
      }
    })
  })

  // Add node-to-node links - only when there are actual connections
  // Green for bidirectional, Red for unidirectional
  const addrToNodeId = new Map()
  const clusterNodes = []
  graphData.goverse_nodes.forEach(n => {
    if (n.advertise_addr) {
      addrToNodeId.set(n.advertise_addr, n.id)
    }
    clusterNodes.push({
      id: n.id,
      advertiseAddr: n.advertise_addr,
      connectedNodes: n.connected_nodes || [],
      linkMetrics: n.link_metrics || {}
    })
  })

  // Create links only for actual connections
  for (let i = 0; i < clusterNodes.length; i++) {
    for (let j = i + 1; j < clusterNodes.length; j++) {
      const nodeA = clusterNodes[i]
      const nodeB = clusterNodes[j]
      
      // Check if A connects to B
      const aConnectsToB = nodeA.connectedNodes.includes(nodeB.advertiseAddr)
      // Check if B connects to A
      const bConnectsToA = nodeB.connectedNodes.includes(nodeA.advertiseAddr)
      
      // Only create link if there's at least one connection
      if (aConnectsToB || bConnectsToA) {
        let linkColor
        if (aConnectsToB && bConnectsToA) {
          // Dual connection: green
          linkColor = '#4CAF50'
        } else {
          // Single direction: red
          linkColor = '#F44336'
        }
        
        // Get calls per minute for this link
        const aToBCpm = nodeA.linkMetrics[nodeB.advertiseAddr] || 0
        const bToACpm = nodeB.linkMetrics[nodeA.advertiseAddr] || 0
        const totalCpm = aToBCpm + bToACpm
        
        links.push({
          source: nodeA.id,
          target: nodeB.id,
          type: 'node-node',
          color: linkColor,
          callsPerMinute: totalCpm
        })
      }
    }
  }

  // Add gate-to-node links
  // Gates connect to nodes (gate's connectedNodes is a list of node addresses)
  graphData.goverse_gates.forEach(gate => {
    const gateConnectedNodes = gate.connected_nodes || []
    const gateLinkMetrics = gate.link_metrics || {}
    gateConnectedNodes.forEach(nodeAddr => {
      const nodeId = addrToNodeId.get(nodeAddr)
      if (nodeId && nodeMap.has(nodeId)) {
        const cpm = gateLinkMetrics[nodeAddr] || 0
        links.push({
          source: gate.id,
          target: nodeId,
          type: 'gate-node',
          color: '#2196F3', // Blue for gate-to-node connections
          callsPerMinute: cpm
        })
      }
    })
  })

  return { nodes, links, nodeMap }
}

// Update the graph with current data
function updateGraph() {
  const { nodes, links } = buildGraphNodesAndLinks()

  // Update simulation
  simulation.nodes(nodes)
  simulation.force('link').links(links)

  // Update links
  const linkSelection = g.select('.links')
    .selectAll('.graph-link')
    .data(links, d => `${d.source.id || d.source}-${d.target.id || d.target}`)

  linkSelection.exit().remove()

  const linkEnter = linkSelection.enter()
    .append('line')

  // Merge and update all links with dynamic styling
  linkEnter.merge(linkSelection)
    .attr('class', d => getLinkClasses(d))
    .attr('stroke', d => getLinkColor(d))
    .attr('stroke-width', d => getLinkStrokeWidth(d))
    .attr('stroke-opacity', d => {
      // Higher opacity for active links
      if ((d.type === 'node-node' || d.type === 'gate-node') && d.callsPerMinute > 0) {
        return 0.8
      }
      return (d.type === 'node-node' || d.type === 'gate-node') ? 0.6 : 1
    })
    .attr('marker-end', d => {
      // Add arrow markers for bidirectional node-node links
      if (d.type === 'node-node' && d.callsPerMinute > 0) {
        return 'url(#arrow)'
      }
      return null
    })

  // Update nodes
  const nodeSelection = g.select('.nodes')
    .selectAll('.graph-node')
    .data(nodes, d => d.id)

  nodeSelection.exit().remove()

  const nodeEnter = nodeSelection.enter()
    .append('g')
    .attr('class', 'graph-node')
    .call(d3.drag()
      .on('start', dragStarted)
      .on('drag', dragged)
      .on('end', dragEnded))
    .on('mouseover', showTooltip)
    .on('mousemove', moveTooltip)
    .on('mouseout', hideTooltip)
    .on('click', (event, d) => {
      event.stopPropagation()
      showDetailsPanel(d)
    })

  // Add shapes based on node type
  nodeEnter.each(function(d) {
    const el = d3.select(this)
    const r = getNodeRadius(d)
    const shape = getNodeShape(d)

    if (shape === 'square') {
      el.append('rect')
        .attr('width', r * 2)
        .attr('height', r * 2)
        .attr('x', -r)
        .attr('y', -r)
        .attr('rx', 4)
        .attr('fill', getNodeColor(d))
    } else if (shape === 'diamond') {
      el.append('polygon')
        .attr('points', `0,${-r} ${r},0 0,${r} ${-r},0`)
        .attr('fill', getNodeColor(d))
    } else if (shape === 'hexagon') {
      // Hexagon shape for shards
      const hexPoints = []
      for (let i = 0; i < 6; i++) {
        const angle = (Math.PI / 3) * i - Math.PI / 2
        hexPoints.push(`${r * Math.cos(angle)},${r * Math.sin(angle)}`)
      }
      el.append('polygon')
        .attr('points', hexPoints.join(' '))
        .attr('fill', getNodeColor(d))
      // Add shard label
      el.append('text')
        .attr('class', 'shard-label')
        .attr('text-anchor', 'middle')
        .attr('dy', '0.35em')
        .attr('fill', 'white')
        .attr('font-size', '9px')
        .attr('font-weight', 'bold')
        .attr('pointer-events', 'none')
        .text(d.label)
    } else {
      el.append('circle')
        .attr('r', r)
        .attr('fill', getNodeColor(d))
    }
    
    // Add text label for objects showing first 2 chars of type
    if (d.nodeType === NODE_TYPE_OBJECT && d.type) {
      el.append('text')
        .attr('class', 'object-type-label')
        .attr('text-anchor', 'middle')
        .attr('dy', '0.35em')
        .attr('fill', 'white')
        .attr('font-size', '10px')
        .attr('font-weight', 'bold')
        .attr('pointer-events', 'none')
        .text(d.type.substring(0, 2))
    }
  })

  // Update existing node colors and sizes
  nodeSelection.each(function(d) {
    const el = d3.select(this)
    const r = getNodeRadius(d)
    const shape = getNodeShape(d)
    if (shape === 'square') {
      el.select('rect')
        .attr('fill', getNodeColor(d))
        .attr('width', r * 2)
        .attr('height', r * 2)
        .attr('x', -r)
        .attr('y', -r)
    } else if (shape === 'diamond') {
      el.select('polygon')
        .attr('fill', getNodeColor(d))
        .attr('points', `0,${-r} ${r},0 0,${r} ${-r},0`)
    } else if (shape === 'hexagon') {
      const hexPoints = []
      for (let i = 0; i < 6; i++) {
        const angle = (Math.PI / 3) * i - Math.PI / 2
        hexPoints.push(`${r * Math.cos(angle)},${r * Math.sin(angle)}`)
      }
      el.select('polygon')
        .attr('fill', getNodeColor(d))
        .attr('points', hexPoints.join(' '))
    } else {
      el.select('circle')
        .attr('fill', getNodeColor(d))
        .attr('r', r)
    }
  })

  // Apply highlighting class to new objects
  applyNewObjectHighlighting(nodeEnter.merge(nodeSelection))

  // Update labels
  updateNodeLabels(nodes)
  updateObjectMetricLabels(nodes)
  updateLinkLabels(links)

  // Restart simulation
  simulation.alpha(SIMULATION_ALPHA_FULL).restart()
}

// Incremental graph update - preserves node positions
function updateGraphIncremental() {
  // Get current nodes from simulation to preserve positions
  const currentNodes = simulation.nodes()
  const positionMap = new Map()
  currentNodes.forEach(n => {
    positionMap.set(n.id, { x: n.x, y: n.y, vx: n.vx, vy: n.vy, fx: n.fx, fy: n.fy })
  })

  // Build nodes array: cluster nodes/gates + objects
  const nodes = []
  const links = []
  const nodeMap = new Map()

  // Add cluster nodes
  graphData.goverse_nodes.forEach(n => {
    const existingPos = positionMap.get(n.id)
    const node = {
      id: n.id,
      label: n.label || n.id,
      nodeType: NODE_TYPE_NODE,
      advertiseAddr: n.advertise_addr,
      color: n.color,
      objectCount: n.object_count || 0,
      connectedNodes: n.connected_nodes || [],
      // Preserve position if exists
      x: existingPos ? existingPos.x : undefined,
      y: existingPos ? existingPos.y : undefined,
      vx: existingPos ? existingPos.vx : undefined,
      vy: existingPos ? existingPos.vy : undefined,
      fx: existingPos ? existingPos.fx : undefined,
      fy: existingPos ? existingPos.fy : undefined
    }
    nodes.push(node)
    nodeMap.set(n.id, node)
  })

  // Add gates
  graphData.goverse_gates.forEach((g, index) => {
    const existingPos = positionMap.get(g.id)
    const node = {
      id: g.id,
      label: g.label || g.id,
      nodeType: NODE_TYPE_GATE,
      advertiseAddr: g.advertise_addr,
      color: g.color,
      connectedNodes: g.connected_nodes || [],
      // Preserve position if exists
      x: existingPos ? existingPos.x : undefined,
      y: existingPos ? existingPos.y : undefined,
      vx: existingPos ? existingPos.vx : undefined,
      vy: existingPos ? existingPos.vy : undefined,
      fx: existingPos ? existingPos.fx : undefined,
      fy: existingPos ? existingPos.fy : undefined
    }
    nodes.push(node)
    nodeMap.set(g.id, node)
  })

  // Track shards and their nodes (a shard can be on multiple nodes during migration)
  const shardToNodes = new Map() // shardId -> Set of nodeIds

  // Add objects and track their shards
  graphData.goverse_objects.forEach(obj => {
    const existingPos = positionMap.get(obj.id)
    const node = {
      id: obj.id,
      label: obj.label || obj.id,
      nodeType: NODE_TYPE_OBJECT,
      type: obj.type,
      shardId: obj.shard_id,
      goverseNodeId: obj.goverse_node_id,
      color: obj.type ? stringToColor(obj.type) : typeColors.default, // Compute color from type
      size: obj.size,
      callsPerMinute: obj.calls_per_minute,
      avgExecutionDurationUs: obj.avg_execution_duration_us,
      // Preserve position if exists
      x: existingPos ? existingPos.x : undefined,
      y: existingPos ? existingPos.y : undefined,
      vx: existingPos ? existingPos.vx : undefined,
      vy: existingPos ? existingPos.vy : undefined,
      fx: existingPos ? existingPos.fx : undefined,
      fy: existingPos ? existingPos.fy : undefined
    }
    nodes.push(node)
    nodeMap.set(obj.id, node)

    // Track which nodes have objects for each shard (skip fixed-node objects with shard_id = -1)
    if (obj.shard_id !== undefined && obj.shard_id !== -1 && obj.goverse_node_id) {
      if (!shardToNodes.has(obj.shard_id)) {
        shardToNodes.set(obj.shard_id, new Set())
      }
      shardToNodes.get(obj.shard_id).add(obj.goverse_node_id)
    }
  })

  // Create shard nodes and object-to-shard links
  const shardNodeMap = new Map()
  graphData.goverse_objects.forEach(obj => {
    // Fixed-node objects (shard_id = -1) should link directly to their node
    if (obj.shard_id === -1 && obj.goverse_node_id && nodeMap.has(obj.goverse_node_id)) {
      links.push({
        source: obj.id,
        target: obj.goverse_node_id,
        type: 'object-node',
        color: '#00BCD4' // Cyan for fixed-node object-to-node
      })
    } else if (obj.shard_id !== undefined && obj.shard_id !== -1) {
      const shardId = `shard-${obj.shard_id}`
      
      // Create shard node if not exists
      if (!shardNodeMap.has(shardId)) {
        const existingPos = positionMap.get(shardId)
        const shardNode = {
          id: shardId,
          label: `#${obj.shard_id}`,
          nodeType: NODE_TYPE_SHARD,
          shardId: obj.shard_id,
          // Preserve position if exists
          x: existingPos ? existingPos.x : undefined,
          y: existingPos ? existingPos.y : undefined,
          vx: existingPos ? existingPos.vx : undefined,
          vy: existingPos ? existingPos.vy : undefined,
          fx: existingPos ? existingPos.fx : undefined,
          fy: existingPos ? existingPos.fy : undefined
        }
        nodes.push(shardNode)
        nodeMap.set(shardId, shardNode)
        shardNodeMap.set(shardId, shardNode)
      }

      // Link object to shard
      links.push({
        source: obj.id,
        target: shardId,
        type: 'object-shard',
        color: '#9C27B0' // Purple for object-to-shard
      })
    }
  })

  // Create shard-to-node links
  shardToNodes.forEach((nodeIds, shardId) => {
    const shardNodeId = `shard-${shardId}`
    const isMultiNode = nodeIds.size > 1 // Shard split across multiple nodes (error state)
    
    nodeIds.forEach(nodeId => {
      if (nodeMap.has(nodeId)) {
        links.push({
          source: shardNodeId,
          target: nodeId,
          type: 'shard-node',
          color: isMultiNode ? '#F44336' : '#FFA726' // Red if split, orange otherwise
        })
      }
    })
  })

  // Add node-to-node links - only when there are actual connections
  // Green for bidirectional, Red for unidirectional
  const addrToNodeId = new Map()
  const clusterNodes = []
  graphData.goverse_nodes.forEach(n => {
    if (n.advertise_addr) {
      addrToNodeId.set(n.advertise_addr, n.id)
    }
    clusterNodes.push({
      id: n.id,
      advertiseAddr: n.advertise_addr,
      connectedNodes: n.connected_nodes || [],
      linkMetrics: n.link_metrics || {}
    })
  })

  // Create links only for actual connections
  for (let i = 0; i < clusterNodes.length; i++) {
    for (let j = i + 1; j < clusterNodes.length; j++) {
      const nodeA = clusterNodes[i]
      const nodeB = clusterNodes[j]
      
      // Check if A connects to B
      const aConnectsToB = nodeA.connectedNodes.includes(nodeB.advertiseAddr)
      // Check if B connects to A
      const bConnectsToA = nodeB.connectedNodes.includes(nodeA.advertiseAddr)
      
      // Only create link if there's at least one connection
      if (aConnectsToB || bConnectsToA) {
        let linkColor
        if (aConnectsToB && bConnectsToA) {
          // Dual connection: green
          linkColor = '#4CAF50'
        } else {
          // Single direction: red
          linkColor = '#F44336'
        }
        
        // Get calls per minute for this link
        const aToBCpm = nodeA.linkMetrics[nodeB.advertiseAddr] || 0
        const bToACpm = nodeB.linkMetrics[nodeA.advertiseAddr] || 0
        const totalCpm = aToBCpm + bToACpm
        
        links.push({
          source: nodeA.id,
          target: nodeB.id,
          type: 'node-node',
          color: linkColor,
          callsPerMinute: totalCpm
        })
      }
    }
  }

  // Add gate-to-node links
  // Gates connect to nodes (gate's connectedNodes is a list of node addresses)
  graphData.goverse_gates.forEach(gate => {
    const gateConnectedNodes = gate.connected_nodes || []
    const gateLinkMetrics = gate.link_metrics || {}
    gateConnectedNodes.forEach(nodeAddr => {
      const nodeId = addrToNodeId.get(nodeAddr)
      if (nodeId && nodeMap.has(nodeId)) {
        const cpm = gateLinkMetrics[nodeAddr] || 0
        links.push({
          source: gate.id,
          target: nodeId,
          type: 'gate-node',
          color: '#2196F3', // Blue for gate-to-node connections
          callsPerMinute: cpm
        })
      }
    })
  })

  // Update simulation with low alpha to minimize movement
  simulation.nodes(nodes)
  simulation.force('link').links(links)

  // Update links
  const linkSelection = g.select('.links')
    .selectAll('.graph-link')
    .data(links, d => `${d.source.id || d.source}-${d.target.id || d.target}`)

  linkSelection.exit().remove()

  const linkEnter = linkSelection.enter()
    .append('line')

  // Merge and update all links with dynamic styling
  linkEnter.merge(linkSelection)
    .attr('class', d => getLinkClasses(d))
    .attr('stroke', d => getLinkColor(d))
    .attr('stroke-width', d => getLinkStrokeWidth(d))
    .attr('stroke-opacity', d => {
      // Higher opacity for active links
      if ((d.type === 'node-node' || d.type === 'gate-node') && d.callsPerMinute > 0) {
        return 0.8
      }
      return (d.type === 'node-node' || d.type === 'gate-node') ? 0.6 : 1
    })
    .attr('marker-end', d => {
      // Add arrow markers for bidirectional node-node links
      if (d.type === 'node-node' && d.callsPerMinute > 0) {
        return 'url(#arrow)'
      }
      return null
    })

  // Update nodes
  const nodeSelection = g.select('.nodes')
    .selectAll('.graph-node')
    .data(nodes, d => d.id)

  nodeSelection.exit().remove()

  const nodeEnter = nodeSelection.enter()
    .append('g')
    .attr('class', 'graph-node')
    .call(d3.drag()
      .on('start', dragStarted)
      .on('drag', dragged)
      .on('end', dragEnded))
    .on('mouseover', showTooltip)
    .on('mousemove', moveTooltip)
    .on('mouseout', hideTooltip)
    .on('click', (event, d) => {
      event.stopPropagation()
      showDetailsPanel(d)
    })

  // Update click handlers on existing nodes
  nodeSelection
    .on('click', (event, d) => {
      event.stopPropagation()
      showDetailsPanel(d)
    })

  // Add shapes based on node type
  nodeEnter.each(function(d) {
    const el = d3.select(this)
    const r = getNodeRadius(d)
    const shape = getNodeShape(d)

    if (shape === 'square') {
      el.append('rect')
        .attr('width', r * 2)
        .attr('height', r * 2)
        .attr('x', -r)
        .attr('y', -r)
        .attr('rx', 4)
        .attr('fill', getNodeColor(d))
    } else if (shape === 'diamond') {
      el.append('polygon')
        .attr('points', `0,${-r} ${r},0 0,${r} ${-r},0`)
        .attr('fill', getNodeColor(d))
    } else if (shape === 'hexagon') {
      // Hexagon shape for shards
      const hexPoints = []
      for (let i = 0; i < 6; i++) {
        const angle = (Math.PI / 3) * i - Math.PI / 2
        hexPoints.push(`${r * Math.cos(angle)},${r * Math.sin(angle)}`)
      }
      el.append('polygon')
        .attr('points', hexPoints.join(' '))
        .attr('fill', getNodeColor(d))
      // Add shard label
      el.append('text')
        .attr('class', 'shard-label')
        .attr('text-anchor', 'middle')
        .attr('dy', '0.35em')
        .attr('fill', 'white')
        .attr('font-size', '9px')
        .attr('font-weight', 'bold')
        .attr('pointer-events', 'none')
        .text(d.label)
    } else {
      el.append('circle')
        .attr('r', r)
        .attr('fill', getNodeColor(d))
    }
    
    // Add text label for objects showing first 2 chars of type
    if (d.nodeType === NODE_TYPE_OBJECT && d.type) {
      el.append('text')
        .attr('class', 'object-type-label')
        .attr('text-anchor', 'middle')
        .attr('dy', '0.35em')
        .attr('fill', 'white')
        .attr('font-size', '10px')
        .attr('font-weight', 'bold')
        .attr('pointer-events', 'none')
        .text(d.type.substring(0, 2))
    }
  })

  // Update existing node colors and sizes
  nodeSelection.each(function(d) {
    const el = d3.select(this)
    const r = getNodeRadius(d)
    const shape = getNodeShape(d)
    if (shape === 'square') {
      el.select('rect')
        .attr('fill', getNodeColor(d))
        .attr('width', r * 2)
        .attr('height', r * 2)
        .attr('x', -r)
        .attr('y', -r)
    } else if (shape === 'diamond') {
      el.select('polygon')
        .attr('fill', getNodeColor(d))
        .attr('points', `0,${-r} ${r},0 0,${r} ${-r},0`)
    } else if (shape === 'hexagon') {
      const hexPoints = []
      for (let i = 0; i < 6; i++) {
        const angle = (Math.PI / 3) * i - Math.PI / 2
        hexPoints.push(`${r * Math.cos(angle)},${r * Math.sin(angle)}`)
      }
      el.select('polygon')
        .attr('fill', getNodeColor(d))
        .attr('points', hexPoints.join(' '))
    } else {
      el.select('circle')
        .attr('fill', getNodeColor(d))
        .attr('r', r)
    }
  })

  // Apply highlighting class to new objects
  applyNewObjectHighlighting(nodeEnter.merge(nodeSelection))

  // Update labels
  updateNodeLabels(nodes)
  updateObjectMetricLabels(nodes)
  updateLinkLabels(links)

  // Use very low alpha to minimize disruption
  simulation.alpha(SIMULATION_ALPHA_INCREMENTAL).restart()
}

// Show call popup animation on an object node
function showCallPopup(objectId, method, objectClass) {
  // Only show popups if graph view is active
  if (!document.getElementById('graph-view').classList.contains('active')) {
    return
  }

  // Check if graph is initialized
  if (!g || !simulation) {
    return
  }

  // Find the node in the simulation
  const node = simulation.nodes().find(n => n.id === objectId)
  if (!node || node.nodeType !== NODE_TYPE_OBJECT) {
    return
  }

  // Create popup element
  const popup = document.createElement('div')
  popup.className = 'call-popup'
  popup.textContent = method
  
  // Add to container
  const container = document.getElementById('graph-container')
  const svgElement = container.querySelector('svg')
  if (svgElement) {
    // Position relative to the SVG coordinate system
    // Get the current transform on the g element
    let scale = 1
    let translateX = 0
    let translateY = 0
    
    try {
      const gNode = g.node()
      if (gNode && gNode.transform && gNode.transform.baseVal) {
        const transform = gNode.transform.baseVal.consolidate()
        if (transform) {
          const matrix = transform.matrix
          scale = matrix.a
          translateX = matrix.e
          translateY = matrix.f
        }
      }
    } catch (e) {
      // Ignore transform errors and use default values
      console.debug('Could not get graph transform:', e)
    }
    
    // Calculate screen position
    const screenX = node.x * scale + translateX
    const screenY = node.y * scale + translateY
    
    popup.style.left = `${screenX}px`
    popup.style.top = `${screenY - 20}px`
    container.appendChild(popup)
    
    // Remove after animation completes
    setTimeout(() => {
      // Use remove() with fallback for older browsers
      if (popup.remove) {
        popup.remove()
      } else if (popup.parentNode) {
        popup.parentNode.removeChild(popup)
      }
    }, CALL_POPUP_DURATION)
  }
}

// Toggle object metric labels visibility
function toggleObjectMetricLabels(visible) {
  showObjectMetricLabels = visible
  const { nodes, links } = buildGraphNodesAndLinks()
  updateObjectMetricLabels(nodes)
}

// Toggle link labels visibility
function toggleLinkLabels(visible) {
  showLinkLabels = visible
  const { nodes, links } = buildGraphNodesAndLinks()
  updateLinkLabels(links)
}
