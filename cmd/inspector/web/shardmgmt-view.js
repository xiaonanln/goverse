// Inspector Graph - Shard Management View
// Depends on: constants.js (typeColors)

// Global state for shard management data
let shardManagementData = { shards: [], nodes: [] }

// Drag state
let draggedShardId = null
let draggedSourceNode = null

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
  
  // Add drag and drop event listeners
  setupDragAndDrop()
}

// Setup drag and drop event listeners
function setupDragAndDrop() {
  // Make shard badges draggable
  const shardBadges = document.querySelectorAll('.shard-badge')
  shardBadges.forEach(badge => {
    badge.draggable = true
    badge.classList.add('draggable')
    
    badge.addEventListener('dragstart', handleDragStart)
    badge.addEventListener('dragend', handleDragEnd)
  })
  
  // Make node boxes drop targets
  const nodeBoxes = document.querySelectorAll('.node-box')
  nodeBoxes.forEach(box => {
    const content = box.querySelector('.node-box-content')
    if (content) {
      content.classList.add('drop-target')
      content.addEventListener('dragover', handleDragOver)
      content.addEventListener('dragleave', handleDragLeave)
      content.addEventListener('drop', handleDrop)
    }
  })
}

// Handle drag start
function handleDragStart(e) {
  const badge = e.target
  const title = badge.getAttribute('title')
  
  // Parse shard ID from title (format: "Shard #123 - X objects")
  const match = title.match(/Shard #(\d+)/)
  if (!match) return
  
  draggedShardId = parseInt(match[1])
  
  // Find the source node
  const nodeBox = badge.closest('.node-box')
  if (nodeBox) {
    draggedSourceNode = nodeBox.getAttribute('data-node-id')
  }
  
  badge.classList.add('dragging')
  e.dataTransfer.effectAllowed = 'move'
  e.dataTransfer.setData('text/plain', draggedShardId)
}

// Handle drag end
function handleDragEnd(e) {
  e.target.classList.remove('dragging')
  
  // Clean up any drag-over states
  document.querySelectorAll('.drag-over').forEach(el => {
    el.classList.remove('drag-over')
  })
}

// Handle drag over
function handleDragOver(e) {
  e.preventDefault()
  e.dataTransfer.dropEffect = 'move'
  
  const dropTarget = e.currentTarget
  dropTarget.classList.add('drag-over')
}

// Handle drag leave
function handleDragLeave(e) {
  const dropTarget = e.currentTarget
  dropTarget.classList.remove('drag-over')
}

// Handle drop
function handleDrop(e) {
  e.preventDefault()
  
  const dropTarget = e.currentTarget
  dropTarget.classList.remove('drag-over')
  
  // Find the target node ID
  const nodeBox = dropTarget.closest('.node-box')
  if (!nodeBox) return
  
  const targetNode = nodeBox.getAttribute('data-node-id')
  
  // Don't do anything if dropped on the same node
  if (targetNode === draggedSourceNode) {
    console.log('Dropped on same node, no action needed')
    return
  }
  
  // Show confirmation modal
  showShardMoveConfirmation(draggedShardId, draggedSourceNode, targetNode)
}

// Show confirmation modal for shard movement
function showShardMoveConfirmation(shardId, sourceNode, targetNode) {
  const modal = document.getElementById('shard-move-modal')
  const modalBody = document.getElementById('modal-body-text')
  const confirmBtn = document.getElementById('modal-confirm')
  const cancelBtn = document.getElementById('modal-cancel')
  
  modalBody.innerHTML = `
    <p>Are you sure you want to move shard <strong>#${shardId}</strong>?</p>
    <p>From: <code>${sourceNode}</code></p>
    <p>To: <code>${targetNode}</code></p>
    <p><em>This will update the target node in etcd. The shard will migrate once the target node claims it.</em></p>
  `
  
  // Remove old event listeners by cloning buttons
  const newConfirmBtn = confirmBtn.cloneNode(true)
  const newCancelBtn = cancelBtn.cloneNode(true)
  confirmBtn.parentNode.replaceChild(newConfirmBtn, confirmBtn)
  cancelBtn.parentNode.replaceChild(newCancelBtn, cancelBtn)
  
  // Add new event listeners
  newConfirmBtn.addEventListener('click', () => {
    modal.classList.remove('visible')
    moveShardToNode(shardId, targetNode)
  })
  
  newCancelBtn.addEventListener('click', () => {
    modal.classList.remove('visible')
  })
  
  // Show modal
  modal.classList.add('visible')
  
  // Close modal on overlay click
  modal.addEventListener('click', (e) => {
    if (e.target === modal) {
      modal.classList.remove('visible')
    }
  })
}

// Move shard to target node via API
function moveShardToNode(shardId, targetNode) {
  fetch('/shards/move', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({
      shard_id: shardId,
      target_node: targetNode
    })
  })
  .then(response => {
    if (!response.ok) {
      return response.text().then(text => {
        throw new Error(text || `HTTP error ${response.status}`)
      })
    }
    return response.json()
  })
  .then(data => {
    console.log('Shard moved successfully:', data)
    // Show success message (optional - could add a toast notification)
    alert(`Shard #${shardId} target updated to ${targetNode}. Migration will start automatically.`)
    // Refresh the view
    updateShardManagementView()
  })
  .catch(error => {
    console.error('Failed to move shard:', error)
    alert(`Failed to move shard: ${error.message}`)
  })
}
