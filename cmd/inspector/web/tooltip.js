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

// Show tooltip for nodes view (includes object count and connections)
function showNodesViewTooltip(event, d) {
  let content = `<div class="tooltip-title">${d.label || d.id}</div>`
  content += `<div class="tooltip-row"><span class="tooltip-label">Type:</span> ${d.nodeType}</div>`
  if (d.advertiseAddr) {
    content += `<div class="tooltip-row"><span class="tooltip-label">Address:</span> ${d.advertiseAddr}</div>`
  }
  content += `<div class="tooltip-row"><span class="tooltip-label">Objects:</span> ${d.objectCount}</div>`
  if (d.connectedNodes && d.connectedNodes.length > 0) {
    content += `<div class="tooltip-row"><span class="tooltip-label">Connected to:</span> ${d.connectedNodes.length} nodes</div>`
  }

  tooltip.innerHTML = content
  tooltip.classList.add('visible')
}
