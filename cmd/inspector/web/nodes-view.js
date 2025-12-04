// Inspector Graph - Nodes View

// Nodes view state
let nodesSimulation = null
let nodesSvg = null
let nodesG = null
let nodesZoom = null
let nodesViewInitialized = false

// Initialize Nodes-only view
function initNodesView() {
  if (nodesViewInitialized) return
  nodesViewInitialized = true

  const container = document.getElementById('nodes-graph-container')
  const width = container.clientWidth || 800
  const height = container.clientHeight || 600

  nodesSvg = d3.select('#nodes-graph-container')
    .append('svg')
    .attr('width', width)
    .attr('height', height)

  // Add zoom behavior
  nodesZoom = d3.zoom()
    .scaleExtent([0.1, 4])
    .on('zoom', (event) => {
      nodesG.attr('transform', event.transform)
    })

  nodesSvg.call(nodesZoom)

  // Main group for zoom/pan
  nodesG = nodesSvg.append('g')

  // Links group (must be before nodes so links appear behind)
  nodesG.append('g').attr('class', 'nodes-only-links')

  // Nodes group
  nodesG.append('g').attr('class', 'nodes-only-nodes')

  // Labels group
  nodesG.append('g').attr('class', 'nodes-only-labels')

  // Initialize force simulation for nodes only
  nodesSimulation = d3.forceSimulation()
    .force('charge', d3.forceManyBody().strength(-300))
    .force('center', d3.forceCenter(width / 2, height / 2))
    .force('collision', d3.forceCollide().radius(60))
    .force('link', d3.forceLink().id(d => d.id).distance(200).strength(0.3))
    .on('tick', nodesViewTicked)

  // Zoom controls for nodes view
  document.getElementById('nodes-zoom-in').addEventListener('click', () => {
    nodesSvg.transition().duration(300).call(nodesZoom.scaleBy, 1.3)
  })
  document.getElementById('nodes-zoom-out').addEventListener('click', () => {
    nodesSvg.transition().duration(300).call(nodesZoom.scaleBy, 0.7)
  })
  document.getElementById('nodes-zoom-reset').addEventListener('click', () => {
    nodesSvg.transition().duration(300).call(nodesZoom.transform, d3.zoomIdentity)
  })
}

// Handle resize for nodes view (called from graph-view.js)
function handleNodesViewResize() {
  if (nodesViewInitialized && nodesSvg) {
    const nodesContainer = document.getElementById('nodes-graph-container')
    const nodesWidth = nodesContainer.clientWidth
    const nodesHeight = nodesContainer.clientHeight
    nodesSvg.attr('width', nodesWidth).attr('height', nodesHeight)
    nodesSimulation.force('center', d3.forceCenter(nodesWidth / 2, nodesHeight / 2))
    nodesSimulation.alpha(0.3).restart()
  }
}

// Tick function for nodes view simulation
function nodesViewTicked() {
  // Update link positions
  nodesG.select('.nodes-only-links').selectAll('.nodes-link')
    .attr('x1', d => d.source.x)
    .attr('y1', d => d.source.y)
    .attr('x2', d => d.target.x)
    .attr('y2', d => d.target.y)

  // Update node positions
  nodesG.select('.nodes-only-nodes').selectAll('.graph-node')
    .attr('transform', d => `translate(${d.x}, ${d.y})`)

  // Update label positions
  nodesG.select('.nodes-only-labels').selectAll('.node-label')
    .attr('x', d => d.x)
    .attr('y', d => d.y + 50)
}

// Drag handlers for nodes view
function nodesViewDragStarted(event, d) {
  if (!event.active) nodesSimulation.alphaTarget(SIMULATION_ALPHA_FULL).restart()
  d.fx = d.x
  d.fy = d.y
}

function nodesViewDragged(event, d) {
  d.fx = event.x
  d.fy = event.y
}

function nodesViewDragEnded(event, d) {
  if (!event.active) nodesSimulation.alphaTarget(0)
  d.fx = null
  d.fy = null
}

// Update nodes-only view
function updateNodesView() {
  if (!nodesViewInitialized) {
    initNodesView()
  }

  // Get current nodes from simulation to preserve positions
  const currentNodes = nodesSimulation.nodes()
  const positionMap = new Map()
  currentNodes.forEach(n => {
    positionMap.set(n.id, { x: n.x, y: n.y, vx: n.vx, vy: n.vy, fx: n.fx, fy: n.fy })
  })

  // Build nodes array: only cluster nodes and gates (no objects)
  const nodes = []
  const nodeMap = new Map()
  const addrToNodeId = new Map() // Map advertise_addr to node id for link resolution

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
    if (n.advertise_addr) {
      addrToNodeId.set(n.advertise_addr, n.id)
    }
  })

  graphData.goverse_gates.forEach(g => {
    const existingPos = positionMap.get(g.id)
    const node = {
      id: g.id,
      label: g.label || g.id,
      nodeType: NODE_TYPE_GATE,
      advertiseAddr: g.advertise_addr,
      color: g.color,
      objectCount: 0,
      clients: g.clients || 0,
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

  // Build links only for actual connections
  // Green for bidirectional, Red for unidirectional
  const links = []
  const nodeList = Array.from(nodeMap.values()).filter(n => n.nodeType === NODE_TYPE_NODE)
  
  // Create links only for node pairs with actual connections
  for (let i = 0; i < nodeList.length; i++) {
    for (let j = i + 1; j < nodeList.length; j++) {
      const nodeA = nodeList[i]
      const nodeB = nodeList[j]
      
      // Check if A connects to B
      const aConnectsToB = nodeA.connectedNodes.includes(nodeB.advertiseAddr)
      // Check if B connects to A
      const bConnectsToA = nodeB.connectedNodes.includes(nodeA.advertiseAddr)
      
      // Only create link if there's at least one connection
      if (aConnectsToB || bConnectsToA) {
        let linkColor, linkWidth, connectionType
        if (aConnectsToB && bConnectsToA) {
          // Dual connection: green
          linkColor = '#4CAF50'
          linkWidth = 2
          connectionType = 'dual'
        } else {
          // Single direction: red
          linkColor = '#F44336'
          linkWidth = 2
          connectionType = 'single'
        }
        
        links.push({
          source: nodeA.id,
          target: nodeB.id,
          color: linkColor,
          width: linkWidth,
          connectionType: connectionType
        })
      }
    }
  }

  // Add gate-to-node links
  // Gates connect to nodes (gate's connectedNodes is a list of node addresses)
  const gateList = Array.from(nodeMap.values()).filter(n => n.nodeType === NODE_TYPE_GATE)
  gateList.forEach(gate => {
    gate.connectedNodes.forEach(nodeAddr => {
      const nodeId = addrToNodeId.get(nodeAddr)
      if (nodeId && nodeMap.has(nodeId)) {
        links.push({
          source: gate.id,
          target: nodeId,
          color: '#2196F3', // Blue for gate-to-node connections
          width: 2,
          connectionType: 'gate-node'
        })
      }
    })
  })

  // Update simulation with nodes and links
  nodesSimulation.nodes(nodes)
  nodesSimulation.force('link').links(links)

  // Update links
  const linkSelection = nodesG.select('.nodes-only-links')
    .selectAll('.nodes-link')
    .data(links, d => `${d.source.id || d.source}-${d.target.id || d.target}`)

  linkSelection.exit().remove()

  const linkEnter = linkSelection.enter()
    .append('line')
    .attr('class', 'nodes-link')

  linkEnter.merge(linkSelection)
    .attr('stroke', d => d.color)
    .attr('stroke-width', d => d.width)
    .attr('stroke-opacity', 0.6)

  // Update nodes
  const nodeSelection = nodesG.select('.nodes-only-nodes')
    .selectAll('.graph-node')
    .data(nodes, d => d.id)

  nodeSelection.exit().remove()

  const nodeEnter = nodeSelection.enter()
    .append('g')
    .attr('class', 'graph-node')
    .call(d3.drag()
      .on('start', nodesViewDragStarted)
      .on('drag', nodesViewDragged)
      .on('end', nodesViewDragEnded))
    .on('mouseover', showNodesViewTooltip)
    .on('mousemove', moveTooltip)
    .on('mouseout', hideTooltip)
    .on('click', (event, d) => {
      event.stopPropagation()
      showDetailsPanel(d)
    })

  // Add shapes based on node type - larger for nodes view
  nodeEnter.each(function(d) {
    const el = d3.select(this)
    const r = 40 // Larger radius for nodes-only view
    const shape = getNodeShape(d)

    if (shape === 'square') {
      el.append('rect')
        .attr('width', r * 2)
        .attr('height', r * 2)
        .attr('x', -r)
        .attr('y', -r)
        .attr('rx', 6)
        .attr('fill', getNodeColor(d))
    } else if (shape === 'diamond') {
      el.append('polygon')
        .attr('points', `0,${-r} ${r},0 0,${r} ${-r},0`)
        .attr('fill', getNodeColor(d))
    }

    // Add count badge inside the node (objects for nodes, clients for gates)
    el.append('text')
      .attr('class', 'object-count-badge')
      .attr('y', 0)
      .text(d => {
        if (d.nodeType === NODE_TYPE_GATE) {
          return (d.clients || 0) + ' clients'
        } else {
          return (d.objectCount || 0) + ' objects'
        }
      })
  })

  // Update existing nodes
  nodeSelection.each(function(d) {
    const el = d3.select(this)
    const badgeText = d.nodeType === NODE_TYPE_GATE 
      ? (d.clients || 0) + ' clients'
      : (d.objectCount || 0) + ' objects'
    el.select('.object-count-badge').text(badgeText)
  })

  // Merge enter and update selections
  const allNodes = nodeEnter.merge(nodeSelection)

  // Update labels
  const labelSelection = nodesG.select('.nodes-only-labels')
    .selectAll('.node-label')
    .data(nodes, d => d.id)

  labelSelection.exit().remove()

  labelSelection.enter()
    .append('text')
    .attr('class', 'node-label')
    .text(d => d.label)

  labelSelection.text(d => d.label)

  // Restart simulation with low alpha to minimize movement
  nodesSimulation.alpha(SIMULATION_ALPHA_INCREMENTAL).restart()
}
