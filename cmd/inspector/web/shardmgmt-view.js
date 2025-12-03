// Inspector Graph - Shard Management View
// Depends on: constants.js (typeColors)

// Global state for shard management data
let shardManagementData = { shards: [], nodes: [] }

// Update shard management view
function updateShardManagementView() {
  const container = document.getElementById('shardmgmt-container')

  // Fetch shard mapping from backend
  fetch('/shards')
    .then(response => response.json())
    .then(data => {
      shardManagementData = data
      renderShardManagementView(container)
    })
    .catch(error => {
      console.error('Failed to fetch shard mapping:', error)
      container.innerHTML = '<div class="shard-error">Failed to load shard mapping. Make sure etcd is configured.</div>'
    })
}

// Render the shard management view
function renderShardManagementView(container) {
  const { shards, nodes } = shardManagementData

  if (!shards || shards.length === 0) {
    container.innerHTML = `
      <div class="shard-empty-state">
        <h3>No Shard Mappings Available</h3>
        <p>Shard mappings are not available. This could be because:</p>
        <ul>
          <li>The inspector is not connected to etcd</li>
          <li>No shards have been assigned yet</li>
          <li>The cluster is not initialized</li>
        </ul>
        <p>To enable shard management, start the inspector with the <code>--etcd-addr</code> flag:</p>
        <pre>./inspector --etcd-addr localhost:2379</pre>
      </div>
    `
    return
  }

  // Group shards by node
  // Display each shard under its CurrentNode (where it currently resides)
  // If CurrentNode is not set, fall back to TargetNode
  const shardsByNode = {}
  nodes.forEach(node => {
    shardsByNode[node] = []
  })

  shards.forEach(shard => {
    const node = shard.current_node || shard.target_node
    if (node) {
      if (!shardsByNode[node]) {
        shardsByNode[node] = []
      }
      shardsByNode[node].push(shard)
    }
  })

  // Sort nodes by ID
  const sortedNodes = nodes.slice().sort()

  // Build HTML
  let html = `
    <div class="shardmgmt-header">
      <h2>Shard Management</h2>
      <div class="shardmgmt-stats">
        <span class="stat-item"><strong>${nodes.length}</strong> Nodes</span>
        <span class="stat-item"><strong>${shards.length}</strong> Shards</span>
      </div>
    </div>
  `

  // Render each node as a full-row box
  sortedNodes.forEach(nodeId => {
    const nodeShards = shardsByNode[nodeId] || []
    
    // Sort shards by object count (descending), then by shard ID (ascending)
    nodeShards.sort((a, b) => {
      const countDiff = (b.object_count || 0) - (a.object_count || 0)
      return countDiff !== 0 ? countDiff : a.shard_id - b.shard_id
    })

    // Determine node color (using the same color scheme as the rest of the UI)
    const nodeColor = typeColors.node

    html += `
      <div class="node-box" data-node-id="${nodeId}">
        <div class="node-box-header" style="background: ${nodeColor}">
          <div class="node-box-title">
            <span class="node-icon">⬛</span>
            <span class="node-id">${nodeId}</span>
          </div>
          <div class="node-box-shard-count">${nodeShards.length} shards</div>
        </div>
        <div class="node-box-content">
          ${nodeShards.length > 0 ? `
            <div class="shard-list">
              ${nodeShards.map(shard => {
                // Check if shard is migrating (both nodes must be set and different)
                const isMigrating = shard.current_node && shard.target_node && 
                                   shard.current_node !== shard.target_node
                const shardClass = isMigrating ? 'shard-badge migrating' : 'shard-badge'
                const objectCount = shard.object_count || 0
                const objectText = objectCount === 1 ? 'object' : 'objects'
                const shardTitle = isMigrating 
                  ? `Shard #${shard.shard_id} (migrating: ${shard.current_node} → ${shard.target_node}) - ${objectCount} ${objectText}`
                  : `Shard #${shard.shard_id} - ${objectCount} ${objectText}`
                return `<span class="${shardClass}" title="${shardTitle}">#${shard.shard_id} (${objectCount} ${objectText})</span>`
              }).join('')}
            </div>
          ` : `
            <div class="node-box-empty">No shards assigned</div>
          `}
        </div>
      </div>
    `
  })

  container.innerHTML = html
}
