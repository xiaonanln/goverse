// Inspector Graph - Objects View (Graph View)

// Objects view state
let simulation = null
let svg = null
let g = null
let zoom = null

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

  // Main group for zoom/pan
  g = svg.append('g')

  // Links group
  g.append('g').attr('class', 'links')

  // Nodes group
  g.append('g').attr('class', 'nodes')

  // Labels group
  g.append('g').attr('class', 'labels')

  // Initialize force simulation
  simulation = d3.forceSimulation()
    .force('charge', d3.forceManyBody().strength(-200))
    .force('center', d3.forceCenter(width / 2, height / 2))
    .force('collision', d3.forceCollide().radius(d => getNodeRadius(d) + 5))
    .force('link', d3.forceLink().id(d => d.id)
      .distance(d => (d.type === 'node-node' || d.type === 'gate-node') ? 150 : 100)
      .strength(d => d.type === 'gate-node' ? 0.1 : 1))
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

  // Update link positions
  g.select('.links').selectAll('.graph-link')
    .attr('x1', d => d.source.x)
    .attr('y1', d => d.source.y)
    .attr('x2', d => d.target.x)
    .attr('y2', d => d.target.y)
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

// Build nodes and links from graph data
function buildGraphNodesAndLinks() {
  const nodes = []
  const links = []
  const nodeMap = new Map()

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
  graphData.goverse_gates.forEach(g => {
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

  // Add objects
  graphData.goverse_objects.forEach(obj => {
    const node = {
      id: obj.id,
      label: obj.label || obj.id,
      nodeType: NODE_TYPE_OBJECT,
      type: obj.type,
      shardId: obj.shard_id,
      goverseNodeId: obj.goverse_node_id,
      color: obj.color,
      size: obj.size
    }
    nodes.push(node)
    nodeMap.set(obj.id, node)

    // Create link from object to its node
    if (obj.goverse_node_id && nodeMap.has(obj.goverse_node_id)) {
      links.push({
        source: obj.id,
        target: obj.goverse_node_id,
        type: 'object-node'
      })
    }
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
      connectedNodes: n.connected_nodes || []
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
        
        links.push({
          source: nodeA.id,
          target: nodeB.id,
          type: 'node-node',
          color: linkColor
        })
      }
    }
  }

  // Add gate-to-node links
  // Gates connect to nodes (gate's connectedNodes is a list of node addresses)
  graphData.goverse_gates.forEach(gate => {
    const gateConnectedNodes = gate.connected_nodes || []
    gateConnectedNodes.forEach(nodeAddr => {
      const nodeId = addrToNodeId.get(nodeAddr)
      if (nodeId && nodeMap.has(nodeId)) {
        links.push({
          source: gate.id,
          target: nodeId,
          type: 'gate-node',
          color: '#2196F3' // Blue for gate-to-node connections
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
    .attr('class', 'graph-link')

  // Merge and update all links
  linkEnter.merge(linkSelection)
    .attr('stroke', d => d.color || '#999')
    .attr('stroke-width', d => (d.type === 'node-node' || d.type === 'gate-node') ? 2 : 1.5)
    .attr('stroke-opacity', d => (d.type === 'node-node' || d.type === 'gate-node') ? 0.6 : 1)

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
    } else {
      el.append('circle')
        .attr('r', r)
        .attr('fill', getNodeColor(d))
    }
  })

  // Update existing node colors
  nodeSelection.each(function(d) {
    const el = d3.select(this)
    const shape = getNodeShape(d)
    if (shape === 'square') {
      el.select('rect').attr('fill', getNodeColor(d))
    } else if (shape === 'diamond') {
      el.select('polygon').attr('fill', getNodeColor(d))
    } else {
      el.select('circle').attr('fill', getNodeColor(d))
    }
  })

  // Update labels
  const labelSelection = g.select('.labels')
    .selectAll('.node-label')
    .data(nodes.filter(d => d.nodeType === NODE_TYPE_NODE || d.nodeType === NODE_TYPE_GATE), d => d.id)

  labelSelection.exit().remove()

  labelSelection.enter()
    .append('text')
    .attr('class', 'node-label')
    .text(d => d.label)

  labelSelection.text(d => d.label)

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
  graphData.goverse_gates.forEach(g => {
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

  // Add objects
  graphData.goverse_objects.forEach(obj => {
    const existingPos = positionMap.get(obj.id)
    const node = {
      id: obj.id,
      label: obj.label || obj.id,
      nodeType: NODE_TYPE_OBJECT,
      type: obj.type,
      shardId: obj.shard_id,
      goverseNodeId: obj.goverse_node_id,
      color: obj.color,
      size: obj.size,
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

    // Create link from object to its node
    if (obj.goverse_node_id && nodeMap.has(obj.goverse_node_id)) {
      links.push({
        source: obj.id,
        target: obj.goverse_node_id,
        type: 'object-node'
      })
    }
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
      connectedNodes: n.connected_nodes || []
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
        
        links.push({
          source: nodeA.id,
          target: nodeB.id,
          type: 'node-node',
          color: linkColor
        })
      }
    }
  }

  // Add gate-to-node links
  // Gates connect to nodes (gate's connectedNodes is a list of node addresses)
  graphData.goverse_gates.forEach(gate => {
    const gateConnectedNodes = gate.connected_nodes || []
    gateConnectedNodes.forEach(nodeAddr => {
      const nodeId = addrToNodeId.get(nodeAddr)
      if (nodeId && nodeMap.has(nodeId)) {
        links.push({
          source: gate.id,
          target: nodeId,
          type: 'gate-node',
          color: '#2196F3' // Blue for gate-to-node connections
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
    .attr('class', 'graph-link')

  // Merge and update all links
  linkEnter.merge(linkSelection)
    .attr('stroke', d => d.color || '#999')
    .attr('stroke-width', d => (d.type === 'node-node' || d.type === 'gate-node') ? 2 : 1.5)
    .attr('stroke-opacity', d => (d.type === 'node-node' || d.type === 'gate-node') ? 0.6 : 1)

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
    } else {
      el.append('circle')
        .attr('r', r)
        .attr('fill', getNodeColor(d))
    }
  })

  // Update existing node colors
  nodeSelection.each(function(d) {
    const el = d3.select(this)
    const shape = getNodeShape(d)
    if (shape === 'square') {
      el.select('rect').attr('fill', getNodeColor(d))
    } else if (shape === 'diamond') {
      el.select('polygon').attr('fill', getNodeColor(d))
    } else {
      el.select('circle').attr('fill', getNodeColor(d))
    }
  })

  // Update labels
  const labelSelection = g.select('.labels')
    .selectAll('.node-label')
    .data(nodes.filter(d => d.nodeType === NODE_TYPE_NODE || d.nodeType === NODE_TYPE_GATE), d => d.id)

  labelSelection.exit().remove()

  labelSelection.enter()
    .append('text')
    .attr('class', 'node-label')
    .text(d => d.label)

  labelSelection.text(d => d.label)

  // Use very low alpha to minimize disruption
  simulation.alpha(SIMULATION_ALPHA_INCREMENTAL).restart()
}
