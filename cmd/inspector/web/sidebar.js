// Inspector Graph - Sidebar Management

// Update sidebar node list
function updateNodeList() {
  const nodeList = document.getElementById('node-list')
  nodeList.innerHTML = ''

  // Add nodes
  graphData.goverse_nodes.forEach(node => {
    const li = document.createElement('li')
    li.className = 'node-item'
    const typeClass = 'node'
    li.innerHTML = `
      <div class="node-id">
        ${node.id}
        <span class="node-type ${typeClass}">${typeClass}</span>
      </div>
      <div class="node-details">
        ${node.advertise_addr ? `Address: ${node.advertise_addr}<br>` : ''}
        ${node.label && node.label !== node.id ? `Label: ${node.label}` : ''}
      </div>
    `

    li.addEventListener('click', (event) => {
      if (event.shiftKey) {
        // Shift+click to show details
        const nodeData = {
          id: node.id,
          label: node.label || node.id,
          nodeType: NODE_TYPE_NODE,
          advertiseAddr: node.advertise_addr,
          color: node.color,
          objectCount: node.object_count || 0,
          connectedNodes: node.connected_nodes || []
        }
        showDetailsPanel(nodeData)
      } else {
        // Normal click to focus
        focusOnNode(node.id)
      }
    })

    nodeList.appendChild(li)
  })

  // Add gates
  graphData.goverse_gates.forEach(gate => {
    const li = document.createElement('li')
    li.className = 'node-item'
    const typeClass = 'gate'
    li.innerHTML = `
      <div class="node-id">
        ${gate.id}
        <span class="node-type ${typeClass}">${typeClass}</span>
      </div>
      <div class="node-details">
        ${gate.advertise_addr ? `Address: ${gate.advertise_addr}<br>` : ''}
        ${gate.label && gate.label !== gate.id ? `Label: ${gate.label}` : ''}
      </div>
    `

    li.addEventListener('click', (event) => {
      if (event.shiftKey) {
        // Shift+click to show details
        const gateData = {
          id: gate.id,
          label: gate.label || gate.id,
          nodeType: NODE_TYPE_GATE,
          advertiseAddr: gate.advertise_addr,
          color: gate.color,
          connectedNodes: gate.connected_nodes || []
        }
        showDetailsPanel(gateData)
      } else {
        // Normal click to focus
        focusOnNode(gate.id)
      }
    })

    nodeList.appendChild(li)
  })
}
