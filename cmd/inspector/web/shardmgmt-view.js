// Inspector Graph - Shard Management View
// Depends on: constants.js (typeColors)

// Global state for shard management data
let shardManagementData = { shards: [], nodes: [] }

// Drag state
let draggedShardId = null
let draggedSourceNode = null

// Track recently moved shards for highlighting (shardId -> timestamp)
let recentlyMovedShards = new Map()

// Track shards that were migrating in previous state (for detecting completion)
let previousMigratingShards = new Map() // shardId -> targetNode

// Calculate proportional badge scale based on object count
// Returns a scale factor between 0.85 (min) and 1.4 (max)
function getShardBadgeScale(objectCount, maxObjectCount) {
  const minScale = 0.85
  const maxScale = 1.4
  
  if (maxObjectCount <= 0) return 1.0
  
  // Use square root for more balanced scaling (prevents huge badges)
  const ratio = Math.sqrt(objectCount) / Math.sqrt(maxObjectCount)
  return minScale + ratio * (maxScale - minScale)
}

// Detect migration completions by comparing new state with previous state
function detectMigrationCompletions(newShards) {
  if (!newShards) return
  
  const newState = new Map()
  newShards.forEach(shard => {
    newState.set(shard.shard_id, shard)
  })
  
  // Check each previously migrating shard
  previousMigratingShards.forEach((targetNode, shardId) => {
    const newShard = newState.get(shardId)
    if (newShard && newShard.current_node === targetNode && newShard.target_node === targetNode) {
      // Migration completed! Highlight this shard
      console.log(`Shard #${shardId} migration completed to ${targetNode}`)
      highlightMovedShard(shardId)
    }
  })
  
  // Update the migrating shards map for next comparison
  // Keep tracking shards that have a target_node set (even if current_node is empty/unclaimed)
  // This allows us to detect completion even after the shard goes through unclaimed state
  previousMigratingShards.clear()
  newShards.forEach(shard => {
    if (shard.target_node) {
      // Track any shard with a target_node, regardless of current_node
      // This handles the case where shard goes: Node A -> Unclaimed -> Node B
      previousMigratingShards.set(shard.shard_id, shard.target_node)
    }
  })
}

// Update shard management view
function updateShardManagementView() {
  const container = document.getElementById('shardmgmt-container')

  // Initialize modal listeners once
  if (!window.shardModalInitialized) {
    initializeModalListeners()
    window.shardModalInitialized = true
  }

  // Fetch shard mapping from backend
  fetch('/shards')
    .then(response => response.json())
    .then(data => {
      shardManagementData = data
      
      // Initialize previousMigratingShards on first load
      if (previousMigratingShards.size === 0 && data.shards) {
        data.shards.forEach(shard => {
          if (shard.target_node && shard.target_node !== shard.current_node) {
            previousMigratingShards.set(shard.shard_id, shard.target_node)
          }
        })
      }
      
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
  // Shards without a CurrentNode go to "Unassigned"
  const shardsByNode = {}
  const unassignedShards = []
  
  nodes.forEach(node => {
    shardsByNode[node] = []
  })

  shards.forEach(shard => {
    if (shard.current_node) {
      if (!shardsByNode[shard.current_node]) {
        shardsByNode[shard.current_node] = []
      }
      shardsByNode[shard.current_node].push(shard)
    } else {
      // No current node - shard is unassigned
      unassignedShards.push(shard)
    }
  })

  // Sort nodes by ID
  const sortedNodes = nodes.slice().sort()

  // Calculate max object count for proportional sizing
  const maxObjectCount = Math.max(1, ...shards.map(s => s.object_count || 0))

  // Build HTML
  let html = `
    <div class="shardmgmt-header">
      <h2>Shard Management</h2>
      <div class="shardmgmt-stats">
        <span class="stat-item"><strong>${nodes.length}</strong> Nodes</span>
        <span class="stat-item"><strong>${shards.length}</strong> Shards</span>
        ${unassignedShards.length > 0 ? `<span class="stat-item stat-warning"><strong>${unassignedShards.length}</strong> Unclaimed</span>` : ''}
      </div>
    </div>
  `

  // Render Unclaimed box first (only if there are unclaimed shards)
  if (unassignedShards.length > 0) {
    html += renderUnassignedBox(unassignedShards, maxObjectCount)
  }

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
                // Determine shard badge class based on target_node state
                // - No target_node: unassigned (grey)
                // - target_node != current_node: migrating (orange)
                // - target_node == current_node: normal (green)
                let shardClass = 'shard-badge'
                let shardTitle = ''
                const objectCount = shard.object_count || 0
                const objectText = objectCount === 1 ? 'object' : 'objects'
                const isPinned = shard.flags && shard.flags.includes('pinned')
                const pinIndicator = isPinned ? '📌 ' : ''
                
                // Calculate proportional size (scale 0.85 to 1.4 based on object count)
                const sizeScale = getShardBadgeScale(objectCount, maxObjectCount)
                const sizeStyle = `font-size: ${sizeScale}em; padding: ${3 * sizeScale}px ${6 * sizeScale}px;`
                
                if (!shard.target_node) {
                  shardClass = 'shard-badge no-target'
                  shardTitle = `Shard #${shard.shard_id} (no target assigned) - ${objectCount} ${objectText}`
                } else if (shard.target_node !== shard.current_node) {
                  shardClass = 'shard-badge migrating'
                  shardTitle = `Shard #${shard.shard_id} (migrating: ${shard.current_node} → ${shard.target_node}) - ${objectCount} ${objectText}`
                } else {
                  shardTitle = `Shard #${shard.shard_id} - ${objectCount} ${objectText}`
                }
                
                if (isPinned) {
                  shardTitle += ' (pinned)'
                }
                
                // Add highlight class for recently moved shards
                if (recentlyMovedShards.has(shard.shard_id)) {
                  shardClass += ' highlight'
                }
                
                return `<span class="${shardClass}" data-shard-id="${shard.shard_id}" style="${sizeStyle}" title="${shardTitle}">${pinIndicator}#${shard.shard_id} (${objectCount} ${objectText})</span>`
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

// Render the Unclaimed shards box
function renderUnassignedBox(unassignedShards, maxObjectCount) {
  // Sort shards by shard ID
  unassignedShards.sort((a, b) => a.shard_id - b.shard_id)

  return `
    <div class="node-box unassigned-box" data-node-id="">
      <div class="node-box-header" style="background: #757575">
        <div class="node-box-title">
          <span class="node-icon">⚠️</span>
          <span class="node-id">Unclaimed</span>
        </div>
        <div class="node-box-shard-count">${unassignedShards.length} shards</div>
      </div>
      <div class="node-box-content">
        ${unassignedShards.length > 0 ? `
          <div class="shard-list">
            ${unassignedShards.map(shard => {
              const objectCount = shard.object_count || 0
              const objectText = objectCount === 1 ? 'object' : 'objects'
              const isPinned = shard.flags && shard.flags.includes('pinned')
              const pinIndicator = isPinned ? '📌 ' : ''
              let shardTitle = shard.target_node 
                ? `Shard #${shard.shard_id} (target: ${shard.target_node}) - ${objectCount} ${objectText}`
                : `Shard #${shard.shard_id} - ${objectCount} ${objectText}`
              if (isPinned) {
                shardTitle += ' (pinned)'
              }
              const shardClass = shard.target_node ? 'shard-badge migrating' : 'shard-badge unassigned'
              
              // Calculate proportional size
              const sizeScale = getShardBadgeScale(objectCount, maxObjectCount)
              const sizeStyle = `font-size: ${sizeScale}em; padding: ${3 * sizeScale}px ${6 * sizeScale}px;`
              
              return `<span class="${shardClass}" data-shard-id="${shard.shard_id}" style="${sizeStyle}" title="${shardTitle}">${pinIndicator}#${shard.shard_id} (${objectCount} ${objectText})</span>`
            }).join('')}
          </div>
        ` : `
          <div class="node-box-empty">All shards are claimed by nodes</div>
        `}
      </div>
    </div>
  `
}

// Setup drag and drop event listeners
function setupDragAndDrop() {
  // Make shard badges draggable and clickable
  const shardBadges = document.querySelectorAll('.shard-badge')
  shardBadges.forEach(badge => {
    badge.draggable = true
    badge.classList.add('draggable')
    
    // Prevent text selection during drag without blocking drag
    badge.addEventListener('selectstart', (e) => {
      e.preventDefault()
    })
    
    badge.addEventListener('dragstart', handleDragStart)
    badge.addEventListener('dragend', handleDragEnd)
    
    // Click to show details
    badge.addEventListener('click', handleShardClick)
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

// Modal state to track current action
let currentModalAction = null

// Initialize modal event listeners once
function initializeModalListeners() {
  const modal = document.getElementById('shard-move-modal')
  const confirmBtn = document.getElementById('modal-confirm')
  const cancelBtn = document.getElementById('modal-cancel')
  
  confirmBtn.addEventListener('click', () => {
    modal.classList.remove('visible')
    if (currentModalAction) {
      currentModalAction()
      currentModalAction = null
    }
  })
  
  cancelBtn.addEventListener('click', () => {
    modal.classList.remove('visible')
    currentModalAction = null
  })
  
  // Close modal on overlay click
  modal.addEventListener('click', (e) => {
    if (e.target === modal) {
      modal.classList.remove('visible')
      currentModalAction = null
    }
  })
}

// Show confirmation modal for shard movement
function showShardMoveConfirmation(shardId, sourceNode, targetNode) {
  const modal = document.getElementById('shard-move-modal')
  const modalBody = document.getElementById('modal-body-text')
  
  modalBody.innerHTML = `
    <p>Are you sure you want to move shard <strong>#${shardId}</strong>?</p>
    <p>From: <code>${sourceNode}</code></p>
    <p>To: <code>${targetNode}</code></p>
    <p><em>This will update the target node in etcd. The shard will migrate once the target node claims it.</em></p>
  `
  
  // Set the action to be performed on confirm
  currentModalAction = () => moveShardToNode(shardId, targetNode)
  
  // Show modal
  modal.classList.add('visible')
}

// Highlight a recently moved shard and auto-remove highlight after delay
function highlightMovedShard(shardId) {
  recentlyMovedShards.set(shardId, Date.now())
  
  // Remove highlight after 5 seconds
  setTimeout(() => {
    recentlyMovedShards.delete(shardId)
    // Remove the highlight class from the DOM if element exists
    const badge = document.querySelector(`.shard-badge[data-shard-id="${shardId}"]`)
    if (badge) {
      badge.classList.remove('highlight')
    }
  }, 5000)
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
    // Track this shard as recently moved for highlighting
    highlightMovedShard(shardId)
    // Refresh the view to show updated state
    updateShardManagementView()
  })
  .catch(error => {
    console.error('Failed to move shard:', error)
    // Show error in modal body and keep modal open
    const modal = document.getElementById('shard-move-modal')
    const modalBody = document.getElementById('modal-body-text')
    modalBody.innerHTML = `
      <p style="color: #c62828;">Failed to move shard #${shardId}</p>
      <p><strong>Error:</strong> ${error.message}</p>
      <p>Please try again or check the inspector logs for more details.</p>
    `
    setTimeout(() => {
      modal.classList.remove('visible')
    }, 3000)
  })
}

// Handle click on shard badge to show details
function handleShardClick(e) {
  const badge = e.target
  const shardId = parseInt(badge.getAttribute('data-shard-id'))
  
  // Find the shard data
  const shard = shardManagementData.shards.find(s => s.shard_id === shardId)
  if (!shard) return
  
  showShardDetailsPanel(shard)
}

// Show shard details in the details panel
function showShardDetailsPanel(shard) {
  const panel = document.getElementById('details-panel')
  const title = document.getElementById('details-title')
  const content = document.getElementById('details-content')

  // Set title
  title.textContent = `Shard #${shard.shard_id}`

  // Build content
  let html = ''

  // Basic information section
  html += '<div class="detail-section">'
  html += '<h4>Basic Information</h4>'
  html += `<div class="detail-row">`
  html += `<div class="detail-label">Shard ID:</div>`
  html += `<div class="detail-value">${shard.shard_id}</div>`
  html += `</div>`
  html += `<div class="detail-row">`
  html += `<div class="detail-label">Object Count:</div>`
  html += `<div class="detail-value"><strong>${shard.object_count || 0}</strong></div>`
  html += `</div>`
  html += '</div>'

  // Node assignment section
  html += '<div class="detail-section">'
  html += '<h4>Node Assignment</h4>'
  
  // Current node
  html += `<div class="detail-row">`
  html += `<div class="detail-label">Current Node:</div>`
  if (shard.current_node) {
    html += `<div class="detail-value">${shard.current_node}</div>`
  } else {
    html += `<div class="detail-value" style="color: #999; font-style: italic;">Not assigned</div>`
  }
  html += `</div>`
  
  // Target node
  html += `<div class="detail-row">`
  html += `<div class="detail-label">Target Node:</div>`
  if (shard.target_node) {
    html += `<div class="detail-value">${shard.target_node}</div>`
  } else {
    html += `<div class="detail-value" style="color: #999; font-style: italic;">Not assigned</div>`
  }
  html += `</div>`
  
  // Status
  html += `<div class="detail-row">`
  html += `<div class="detail-label">Status:</div>`
  let statusBadge = ''
  if (!shard.target_node) {
    statusBadge = '<span class="detail-badge" style="background: #9E9E9E;">No Target</span>'
  } else if (shard.target_node !== shard.current_node) {
    statusBadge = '<span class="detail-badge" style="background: #FF9800;">Migrating</span>'
  } else {
    statusBadge = '<span class="detail-badge" style="background: #4CAF50;">Stable</span>'
  }
  html += `<div class="detail-value">${statusBadge}</div>`
  html += `</div>`
  
  // Pinned status
  const isPinned = shard.flags && shard.flags.includes('pinned')
  html += `<div class="detail-row">`
  html += `<div class="detail-label">Pinned:</div>`
  html += `<div class="detail-value">`
  if (isPinned) {
    html += `<span class="detail-badge" style="background: #9C27B0;">📌 Pinned</span>`
    html += ` <button class="pin-toggle-btn unpin" onclick="toggleShardPin(${shard.shard_id}, false)">Unpin</button>`
  } else {
    html += `<span class="detail-badge" style="background: #607D8B;">Not Pinned</span>`
    html += ` <button class="pin-toggle-btn pin" onclick="toggleShardPin(${shard.shard_id}, true)">Pin</button>`
  }
  html += `</div>`
  html += `</div>`
  html += '</div>'

  // Objects in this shard (if we have object data)
  if (typeof graphData !== 'undefined' && graphData.goverse_objects) {
    const shardObjects = graphData.goverse_objects.filter(obj => obj.shard_id === shard.shard_id)
    if (shardObjects.length > 0) {
      html += '<div class="detail-section">'
      html += '<h4>Objects in Shard</h4>'
      
      // Group by type
      const typeCount = {}
      shardObjects.forEach(obj => {
        const type = obj.type || 'Unknown'
        typeCount[type] = (typeCount[type] || 0) + 1
      })
      
      html += `<div class="detail-row">`
      html += `<div class="detail-label">By Type:</div>`
      html += `<div class="detail-value">`
      Object.entries(typeCount).sort((a, b) => b[1] - a[1]).forEach(([type, count]) => {
        html += `<div style="margin: 2px 0;">${type}: <strong>${count}</strong></div>`
      })
      html += `</div>`
      html += `</div>`
      
      // List first few objects
      html += `<div class="detail-row">`
      html += `<div class="detail-label">Objects:</div>`
      html += `<div class="detail-value">`
      const displayObjects = shardObjects.slice(0, 10)
      displayObjects.forEach(obj => {
        html += `<div style="margin: 2px 0; font-size: 11px;">${obj.id}</div>`
      })
      if (shardObjects.length > 10) {
        html += `<div style="margin: 2px 0; color: #999; font-style: italic;">... and ${shardObjects.length - 10} more</div>`
      }
      html += `</div>`
      html += `</div>`
      
      html += '</div>'
    }
  }

  content.innerHTML = html
  panel.classList.add('visible')
}

// Toggle pin status for a shard
function toggleShardPin(shardId, pinned) {
  fetch('/shards/pin', {
    method: 'POST',
    headers: {
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({
      shard_id: shardId,
      pinned: pinned
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
    console.log(`Shard ${shardId} ${pinned ? 'pinned' : 'unpinned'} successfully:`, data)
    // Refresh the view to show updated state
    updateShardManagementView()
  })
  .catch(error => {
    console.error(`Failed to ${pinned ? 'pin' : 'unpin'} shard:`, error)
    alert(`Failed to ${pinned ? 'pin' : 'unpin'} shard: ${error.message}`)
  })
}
