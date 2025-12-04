// Inspector Graph - SSE (Server-Sent Events) Module

// SSE connection state
let eventSource = null
let reconnectTimeout = null

// Connect to SSE for push updates
function connectSSE() {
  if (eventSource) {
    eventSource.close()
  }

  console.log('Connecting to SSE...')
  eventSource = new EventSource('/events/stream')

  eventSource.addEventListener('initial', (event) => {
    const data = JSON.parse(event.data)
    console.log('SSE initial state received:', data)
    graphData.goverse_nodes = data.goverse_nodes || []
    graphData.goverse_gates = data.goverse_gates || []
    graphData.goverse_objects = data.goverse_objects || []
    updateNodeList()
    updateGraph()
    if (document.getElementById('shard-view').classList.contains('active')) {
      updateShardView()
    }
    if (document.getElementById('nodes-view').classList.contains('active')) {
      updateNodesView()
    }
  })

  eventSource.addEventListener('node_added', (event) => {
    const data = JSON.parse(event.data)
    console.log('SSE node_added:', data)
    if (data.node) {
      upsertNode(data.node)
      updateNodeList()
      updateGraphIncremental()
      if (document.getElementById('nodes-view').classList.contains('active')) {
        updateNodesView()
      }
    }
  })

  eventSource.addEventListener('node_updated', (event) => {
    const data = JSON.parse(event.data)
    console.log('SSE node_updated:', data)
    if (data.node) {
      upsertNode(data.node)
      updateNodeList()
      if (document.getElementById('nodes-view').classList.contains('active')) {
        updateNodesView()
      }
    }
  })

  eventSource.addEventListener('node_removed', (event) => {
    const data = JSON.parse(event.data)
    console.log('SSE node_removed:', data)
    if (data.node_id) {
      graphData.goverse_nodes = graphData.goverse_nodes.filter(n => n.id !== data.node_id)
      updateNodeList()
      updateGraphIncremental()
      if (document.getElementById('nodes-view').classList.contains('active')) {
        updateNodesView()
      }
    }
  })

  eventSource.addEventListener('gate_added', (event) => {
    const data = JSON.parse(event.data)
    console.log('SSE gate_added:', data)
    if (data.gate) {
      upsertGate(data.gate)
      updateNodeList()
      updateGraphIncremental()
      if (document.getElementById('nodes-view').classList.contains('active')) {
        updateNodesView()
      }
    }
  })

  eventSource.addEventListener('gate_updated', (event) => {
    const data = JSON.parse(event.data)
    console.log('SSE gate_updated:', data)
    if (data.gate) {
      upsertGate(data.gate)
      updateNodeList()
      updateGraphIncremental()
      if (document.getElementById('nodes-view').classList.contains('active')) {
        updateNodesView()
      }
    }
  })

  eventSource.addEventListener('gate_removed', (event) => {
    const data = JSON.parse(event.data)
    console.log('SSE gate_removed:', data)
    if (data.gate_id) {
      graphData.goverse_gates = graphData.goverse_gates.filter(g => g.id !== data.gate_id)
      updateNodeList()
      updateGraphIncremental()
      if (document.getElementById('nodes-view').classList.contains('active')) {
        updateNodesView()
      }
    }
  })

  eventSource.addEventListener('object_added', (event) => {
    const data = JSON.parse(event.data)
    console.log('SSE object_added:', data)
    if (data.object) {
      upsertObject(data.object)
      updateGraphIncremental()
      if (document.getElementById('shard-view').classList.contains('active')) {
        updateShardView()
      }
      if (document.getElementById('nodes-view').classList.contains('active')) {
        updateNodesView()
      }
    }
  })

  eventSource.addEventListener('object_updated', (event) => {
    const data = JSON.parse(event.data)
    console.log('SSE object_updated:', data)
    if (data.object) {
      upsertObject(data.object)
      // No need to restart simulation for updates
    }
  })

  eventSource.addEventListener('object_removed', (event) => {
    const data = JSON.parse(event.data)
    console.log('SSE object_removed:', data)
    if (data.object_id) {
      graphData.goverse_objects = graphData.goverse_objects.filter(o => o.id !== data.object_id)
      updateGraphIncremental()
      if (document.getElementById('shard-view').classList.contains('active')) {
        updateShardView()
      }
      if (document.getElementById('nodes-view').classList.contains('active')) {
        updateNodesView()
      }
    }
  })

  eventSource.addEventListener('shard_update', (event) => {
    console.log('SSE shard_update received', event)
    
    // Try to detect migration completions by comparing with previous state
    try {
      const data = JSON.parse(event.data)
      if (data.shards && typeof detectMigrationCompletions === 'function') {
        detectMigrationCompletions(data.shards)
      }
    } catch (e) {
      // Ignore parse errors, just refresh the view
    }
    
    // Refresh shard view if it's active
    const shardView = document.getElementById('shard-view')
    if (shardView && shardView.classList.contains('active')) {
      console.log('Updating shard view...')
      updateShardView()
    }
    // Also refresh shard management view if it's active
    const shardMgmtView = document.getElementById('shardmgmt-view')
    if (shardMgmtView && shardMgmtView.classList.contains('active')) {
      console.log('Updating shard management view...')
      updateShardManagementView()
    }
  })

  eventSource.addEventListener('heartbeat', (event) => {
    console.log('SSE heartbeat received')
  })

  eventSource.onerror = (error) => {
    console.error('SSE error:', error)
    if (eventSource.readyState === EventSource.CLOSED) {
      console.log(`SSE connection closed, reconnecting in ${SSE_RECONNECT_DELAY / 1000} seconds...`)
      if (reconnectTimeout) clearTimeout(reconnectTimeout)
      reconnectTimeout = setTimeout(connectSSE, SSE_RECONNECT_DELAY)
    }
  }

  eventSource.onopen = () => {
    console.log('SSE connection established')
  }
}

// Fetch and refresh data (fallback polling method)
async function refresh() {
  try {
    const res = await fetch('/graph', { cache: 'no-cache' })
    const data = await res.json()
    graphData.goverse_nodes = data.goverse_nodes || []
    graphData.goverse_gates = data.goverse_gates || []
    graphData.goverse_objects = data.goverse_objects || []
    console.log('Graph data:', graphData)

    updateNodeList()
    updateGraph()

    // Update shard view if visible
    if (document.getElementById('shard-view').classList.contains('active')) {
      updateShardView()
    }
    // Update nodes view if visible
    if (document.getElementById('nodes-view').classList.contains('active')) {
      updateNodesView()
    }
  } catch (err) {
    console.error('Failed to fetch graph data from /graph:', err.message || err)
  }
}
