// Inspector Graph - Tooltip Module

// Tooltip element reference
const tooltip = document.getElementById('tooltip')

// Show tooltip with content based on node data
function showTooltip(event, d) {
  let content = `<div class="tooltip-title">${d.label || d.id}</div>`

  if (d.nodeType === NODE_TYPE_NODE || d.nodeType === NODE_TYPE_GATE) {
    content += `<div class="tooltip-row"><span class="tooltip-label">Type:</span> ${d.nodeType}</div>`
    if (d.advertiseAddr) {
      content += `<div class="tooltip-row"><span class="tooltip-label">Address:</span> ${d.advertiseAddr}</div>`
    }
  } else {
    content += `<div class="tooltip-row"><span class="tooltip-label">Type:</span> ${d.type || 'Object'}</div>`
    if (d.shardId !== undefined) {
      content += `<div class="tooltip-row"><span class="tooltip-label">Shard:</span> ${d.shardId}</div>`
    }
    if (d.goverseNodeId) {
      content += `<div class="tooltip-row"><span class="tooltip-label">Node:</span> ${d.goverseNodeId}</div>`
    }
  }

  tooltip.innerHTML = content
  tooltip.classList.add('visible')
}

// Move tooltip to follow mouse
function moveTooltip(event) {
  tooltip.style.left = (event.pageX + 15) + 'px'
  tooltip.style.top = (event.pageY + 15) + 'px'
}

// Hide tooltip
function hideTooltip() {
  tooltip.classList.remove('visible')
}

// Show details panel with comprehensive information
function showDetailsPanel(d) {
  const panel = document.getElementById('details-panel')
  const title = document.getElementById('details-title')
  const content = document.getElementById('details-content')

  // For objects, get fresh data from graphData to ensure we have latest metrics
  if (d.nodeType === NODE_TYPE_OBJECT) {
    const freshObj = graphData.goverse_objects.find(o => o.id === d.id)
    if (freshObj) {
      d = {
        ...d,
        callsPerMinute: freshObj.calls_per_minute,
        avgExecutionDurationMs: freshObj.avg_execution_duration_ms
      }
    }
  }

  // Set title
  title.textContent = d.label || d.id

  // Build content based on node type
  let html = ''

  // Basic information section
  html += '<div class="detail-section">'
  html += '<h4>Basic Information</h4>'
  html += `<div class="detail-row">`
  html += `<div class="detail-label">ID:</div>`
  html += `<div class="detail-value">${d.id}</div>`
  html += `</div>`
  html += `<div class="detail-row">`
  html += `<div class="detail-label">Type:</div>`
  html += `<div class="detail-value"><span class="detail-badge ${d.nodeType}">${d.nodeType}</span></div>`
  html += `</div>`
  if (d.label && d.label !== d.id) {
    html += `<div class="detail-row">`
    html += `<div class="detail-label">Label:</div>`
    html += `<div class="detail-value">${d.label}</div>`
    html += `</div>`
  }
  html += '</div>'

  // Type-specific information
  if (d.nodeType === NODE_TYPE_NODE || d.nodeType === NODE_TYPE_GATE) {
    // Network information
    if (d.advertiseAddr) {
      html += '<div class="detail-section">'
      html += '<h4>Network</h4>'
      html += `<div class="detail-row">`
      html += `<div class="detail-label">Address:</div>`
      html += `<div class="detail-value">${d.advertiseAddr}</div>`
      html += `</div>`
      html += '</div>'
    }

    // Connections
    if (d.connectedNodes && d.connectedNodes.length > 0) {
      html += '<div class="detail-section">'
      html += '<h4>Connections</h4>'
      html += `<div class="detail-row">`
      html += `<div class="detail-label">Connected to:</div>`
      html += `<div class="detail-value"><span class="detail-badge connected">${d.connectedNodes.length} nodes</span></div>`
      html += `</div>`
      html += `<ul class="detail-list">`
      d.connectedNodes.forEach(addr => {
        // Try to find the node/gate by address
        const connectedNode = graphData.goverse_nodes.find(n => n.advertise_addr === addr)
        const connectedGate = graphData.goverse_gates.find(g => g.advertise_addr === addr)
        const displayName = connectedNode?.label || connectedGate?.label || addr
        html += `<li>${displayName}</li>`
      })
      html += `</ul>`
      html += '</div>'
    } else {
      html += '<div class="detail-section">'
      html += '<h4>Connections</h4>'
      html += `<div class="detail-empty">No connections</div>`
      html += '</div>'
    }

    // Object count for nodes, client count for gates
    if (d.nodeType === NODE_TYPE_NODE) {
      html += '<div class="detail-section">'
      html += '<h4>Objects</h4>'
      html += `<div class="detail-row">`
      html += `<div class="detail-label">Total Count:</div>`
      html += `<div class="detail-value">${d.objectCount || 0}</div>`
      html += `</div>`
      
      // Show object breakdown by type
      const nodeObjects = graphData.goverse_objects.filter(obj => obj.goverse_node_id === d.id)
      if (nodeObjects.length > 0) {
        const typeCount = {}
        nodeObjects.forEach(obj => {
          const type = obj.type || 'Unknown'
          typeCount[type] = (typeCount[type] || 0) + 1
        })
        html += `<div class="detail-row">`
        html += `<div class="detail-label">By Type:</div>`
        html += `<div class="detail-value">`
        Object.entries(typeCount).sort((a, b) => a[0].localeCompare(b[0])).forEach(([type, count]) => {
          html += `<div style="margin: 2px 0;">${type}: <strong>${count}</strong></div>`
        })
        html += `</div>`
        html += `</div>`
        
        // Show all objects sorted by type then id
        html += `<div class="detail-row">`
        html += `<div class="detail-label">All Objects:</div>`
        html += `<div class="detail-value">`
        html += `<ul class="detail-list">`
        nodeObjects
          .sort((a, b) => {
            const typeA = a.type || 'Unknown'
            const typeB = b.type || 'Unknown'
            if (typeA !== typeB) {
              return typeA.localeCompare(typeB)
            }
            return a.id.localeCompare(b.id)
          })
          .forEach(obj => {
            html += `<li><span style="color: #666;">${obj.type || 'Unknown'}:</span> ${obj.id}</li>`
          })
        html += `</ul>`
        html += `</div>`
        html += `</div>`
      }
      html += '</div>'
    } else if (d.nodeType === NODE_TYPE_GATE) {
      // Client count for gates
      html += '<div class="detail-section">'
      html += '<h4>Clients</h4>'
      html += `<div class="detail-row">`
      html += `<div class="detail-label">Total Count:</div>`
      html += `<div class="detail-value">${d.clients || 0}</div>`
      html += `</div>`
      html += '</div>'
    }
  } else if (d.nodeType === NODE_TYPE_OBJECT) {
    // Object-specific information
    html += '<div class="detail-section">'
    html += '<h4>Object Details</h4>'
    if (d.type) {
      html += `<div class="detail-row">`
      html += `<div class="detail-label">Object Type:</div>`
      html += `<div class="detail-value">${d.type}</div>`
      html += `</div>`
    }
    if (d.shardId !== undefined) {
      html += `<div class="detail-row">`
      html += `<div class="detail-label">Shard ID:</div>`
      html += `<div class="detail-value">${d.shardId}</div>`
      html += `</div>`
    }
    if (d.goverseNodeId) {
      html += `<div class="detail-row">`
      html += `<div class="detail-label">Hosted on:</div>`
      const hostNode = graphData.goverse_nodes.find(n => n.id === d.goverseNodeId)
      const hostLabel = hostNode?.label || d.goverseNodeId
      html += `<div class="detail-value">${hostLabel}</div>`
      html += `</div>`
    }
    if (d.callsPerMinute !== undefined) {
      html += `<div class="detail-row">`
      html += `<div class="detail-label">Calls/min:</div>`
      html += `<div class="detail-value">${d.callsPerMinute}</div>`
      html += `</div>`
    }
    if (d.avgExecutionDurationMs !== undefined) {
      html += `<div class="detail-row">`
      html += `<div class="detail-label">Avg Duration:</div>`
      html += `<div class="detail-value">${d.avgExecutionDurationMs.toFixed(2)} ms</div>`
      html += `</div>`
    }
    if (d.size) {
      html += `<div class="detail-row">`
      html += `<div class="detail-label">Size:</div>`
      html += `<div class="detail-value">${d.size}</div>`
      html += `</div>`
    }
    html += '</div>'
  }

  content.innerHTML = html
  panel.classList.add('visible')
}

// Close details panel
document.getElementById('details-close').addEventListener('click', () => {
  document.getElementById('details-panel').classList.remove('visible')
})

// Close details panel when clicking outside
document.addEventListener('click', (event) => {
  const panel = document.getElementById('details-panel')
  if (panel.classList.contains('visible') && !panel.contains(event.target)) {
    // Check if click is on a graph node or shard badge (which will trigger showDetailsPanel)
    if (!event.target.closest('.graph-node') && !event.target.closest('.node-item') && !event.target.closest('.shard-badge')) {
      panel.classList.remove('visible')
    }
  }
})

// Show tooltip for nodes view (includes object count/clients and connections)
function showNodesViewTooltip(event, d) {
  let content = `<div class="tooltip-title">${d.label || d.id}</div>`
  content += `<div class="tooltip-row"><span class="tooltip-label">Type:</span> ${d.nodeType}</div>`
  if (d.advertiseAddr) {
    content += `<div class="tooltip-row"><span class="tooltip-label">Address:</span> ${d.advertiseAddr}</div>`
  }
  // Show clients for gates, objects for nodes
  if (d.nodeType === NODE_TYPE_GATE) {
    content += `<div class="tooltip-row"><span class="tooltip-label">Clients:</span> ${d.clients || 0}</div>`
  } else {
    content += `<div class="tooltip-row"><span class="tooltip-label">Objects:</span> ${d.objectCount}</div>`
  }
  if (d.connectedNodes && d.connectedNodes.length > 0) {
    content += `<div class="tooltip-row"><span class="tooltip-label">Connected to:</span> ${d.connectedNodes.length} nodes</div>`
  }

  tooltip.innerHTML = content
  tooltip.classList.add('visible')
}
