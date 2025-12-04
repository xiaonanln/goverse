// Inspector Graph - Constants and Configuration

// Node type constants
const NODE_TYPE_NODE = 'node'
const NODE_TYPE_GATE = 'gate'
const NODE_TYPE_OBJECT = 'object'

// Animation constants
const SIMULATION_ALPHA_FULL = 0.3      // Alpha for full graph updates
const SIMULATION_ALPHA_INCREMENTAL = 0.1 // Alpha for incremental updates (lower = less disruption)
const SSE_RECONNECT_DELAY = 3000       // SSE reconnect delay in milliseconds

// Pie chart dimensions
const PIE_CHART_SIZE = 200

// Type colors for nodes and gates
const typeColors = {
  node: '#4CAF50',
  gate: '#2196F3',
  default: '#999'
}

// Generate a consistent color from a string (for object types)
function stringToColor(str) {
  if (!str) return typeColors.default
  
  // Simple hash function
  let hash = 0
  for (let i = 0; i < str.length; i++) {
    hash = str.charCodeAt(i) + ((hash << 5) - hash)
    hash = hash & hash // Convert to 32bit integer
  }
  
  // Generate HSL color with good saturation and lightness for visibility
  const hue = Math.abs(hash % 360)
  const saturation = 65 + (Math.abs(hash >> 8) % 20) // 65-85%
  const lightness = 45 + (Math.abs(hash >> 16) % 15) // 45-60%
  
  return `hsl(${hue}, ${saturation}%, ${lightness}%)`
}

// Application state
const graphData = { goverse_nodes: [], goverse_gates: [], goverse_objects: [] }

// Helper function to upsert a node in the goverse_nodes array
function upsertNode(node) {
  const existingIdx = graphData.goverse_nodes.findIndex(n => n.id === node.id)
  if (existingIdx >= 0) {
    graphData.goverse_nodes[existingIdx] = node
  } else {
    graphData.goverse_nodes.push(node)
  }
}

// Helper function to upsert a gate in the goverse_gates array
function upsertGate(gate) {
  const existingIdx = graphData.goverse_gates.findIndex(g => g.id === gate.id)
  if (existingIdx >= 0) {
    graphData.goverse_gates[existingIdx] = gate
  } else {
    graphData.goverse_gates.push(gate)
  }
}

// Helper function to upsert an object in the goverse_objects array
function upsertObject(obj) {
  const existingIdx = graphData.goverse_objects.findIndex(o => o.id === obj.id)
  if (existingIdx >= 0) {
    graphData.goverse_objects[existingIdx] = obj
  } else {
    graphData.goverse_objects.push(obj)
  }
}

// Utility functions for node rendering
function getNodeRadius(d) {
  if (d.nodeType === NODE_TYPE_NODE) return 25
  if (d.nodeType === NODE_TYPE_GATE) return 22
  return d.size ? Math.max(8, Math.min(20, d.size / 2)) : 12
}

function getNodeColor(d) {
  if (d.nodeType === NODE_TYPE_NODE) return typeColors.node
  if (d.nodeType === NODE_TYPE_GATE) return typeColors.gate
  // For objects: use explicit color if set, otherwise generate from type
  return d.color || (d.type ? stringToColor(d.type) : typeColors.default)
}

function getNodeShape(d) {
  if (d.nodeType === NODE_TYPE_NODE) return 'square'
  if (d.nodeType === NODE_TYPE_GATE) return 'diamond'
  return 'circle'
}
