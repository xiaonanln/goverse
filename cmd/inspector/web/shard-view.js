// Inspector Graph - Shard Distribution View

// Update shard distribution view
function updateShardView() {
  const container = document.getElementById('shard-container')

  // Calculate stats
  const objects = graphData.goverse_objects
  const nodes = graphData.goverse_nodes
  const gates = graphData.goverse_gates

  // Group objects by shard
  const shardMap = new Map()
  objects.forEach(obj => {
    const shardId = obj.shard_id || 0
    if (!shardMap.has(shardId)) {
      shardMap.set(shardId, [])
    }
    shardMap.get(shardId).push(obj)
  })

  // Group objects by type
  const typeMap = new Map()
  objects.forEach(obj => {
    const type = obj.type || 'Unknown'
    if (!typeMap.has(type)) {
      typeMap.set(type, [])
    }
    typeMap.get(type).push(obj)
  })

  // Group objects by node
  const nodeObjMap = new Map()
  objects.forEach(obj => {
    const nodeId = obj.goverse_node_id || 'Unknown'
    if (!nodeObjMap.has(nodeId)) {
      nodeObjMap.set(nodeId, [])
    }
    nodeObjMap.get(nodeId).push(obj)
  })

  // Sort type entries alphabetically by type name (used for both legend and pie chart)
  const sortedTypes = typeMap.size > 0 
    ? Array.from(typeMap.entries()).sort((a, b) => a[0].localeCompare(b[0]))
    : []

  // Build HTML
  let html = `
    <div class="shard-stats">
      <div class="stat-card">
        <div class="stat-value">${nodes.length}</div>
        <div class="stat-label">Nodes</div>
      </div>
      <div class="stat-card">
        <div class="stat-value">${gates.length}</div>
        <div class="stat-label">Gates</div>
      </div>
      <div class="stat-card">
        <div class="stat-value">${objects.length}</div>
        <div class="stat-label">Objects</div>
      </div>
      <div class="stat-card">
        <div class="stat-value">${shardMap.size}</div>
        <div class="stat-label">Active Shards</div>
      </div>
    </div>
  `

  // Objects by type chart - using pie chart
  if (sortedTypes.length > 0) {
    html += `
      <div class="shard-chart">
        <h4>Objects by Type</h4>
        <div class="pie-chart-container">
          <svg id="objects-by-type-pie" class="pie-chart-svg" width="${PIE_CHART_SIZE}" height="${PIE_CHART_SIZE}"></svg>
          <div class="pie-chart-legend">
            ${sortedTypes.map(([type, objs]) => `
              <div class="legend-item">
                <div class="legend-color" style="background: ${typeColors[type] || typeColors.default}"></div>
                <span>${type}: ${objs.length}</span>
              </div>
            `).join('')}
          </div>
        </div>
      </div>
    `
  }

  // Objects by node chart
  if (nodeObjMap.size > 0) {
    // Sort node entries by node ID
    const sortedNodes = Array.from(nodeObjMap.entries()).sort((a, b) => a[0].localeCompare(b[0]))
    html += `
      <div class="shard-chart">
        <h4>Objects by Node</h4>
        <div class="legend">
          ${sortedNodes.map(([nodeId]) => `
            <div class="legend-item">
              <div class="legend-color" style="background: ${typeColors.node}"></div>
              <span>${nodeId}</span>
            </div>
          `).join('')}
        </div>
        <div class="shard-bar-container">
          ${sortedNodes.map(([nodeId, objs]) => `
            <div class="shard-bar" 
                 style="background: ${typeColors.node}; width: ${Math.max(30, objs.length * 3)}px"
                 title="${nodeId}: ${objs.length} objects">
            </div>
          `).join('')}
        </div>
      </div>
    `
  }

  // Shard distribution chart
  if (shardMap.size > 0) {
    const sortedShards = Array.from(shardMap.entries()).sort((a, b) => a[0] - b[0])
    const maxObjects = Math.max(...sortedShards.map(([_, objs]) => objs.length))

    html += `
      <div class="shard-chart">
        <h4>Shard Distribution</h4>
        <div class="shard-distribution-container">
          ${sortedShards.map(([shardId, objs]) => {
            const height = Math.max(8, (objs.length / maxObjects) * 40)
            const color = typeColors.node
            return `
              <div class="shard-bar" 
                   style="background: ${color}; height: ${height}px; width: 12px; align-self: flex-end;"
                   title="Shard ${shardId}: ${objs.length} objects">
              </div>
            `
          }).join('')}
        </div>
        <div class="shard-range-label">
          Shards: ${sortedShards[0]?.[0] || 0} - ${sortedShards[sortedShards.length - 1]?.[0] || 0}
        </div>
      </div>
    `
  }

  container.innerHTML = html

  // Render pie chart for Objects by Type using D3.js
  if (sortedTypes.length > 0) {
    renderObjectsByTypePieChart(sortedTypes)
  }
}

// Render pie chart for objects by type
function renderObjectsByTypePieChart(sortedTypes) {
  const pieData = sortedTypes.map(([type, objs]) => ({
    type: type,
    count: objs.length,
    color: typeColors[type] || typeColors.default
  }))

  const radius = PIE_CHART_SIZE / 2

  const pieSvg = d3.select('#objects-by-type-pie')
  pieSvg.selectAll('*').remove()

  const pieG = pieSvg
    .append('g')
    .attr('transform', `translate(${PIE_CHART_SIZE / 2}, ${PIE_CHART_SIZE / 2})`)

  // Use .sort(null) to preserve alphabetical order from sortedTypes (matching legend order)
  const pie = d3.pie()
    .value(d => d.count)
    .sort(null)

  const arc = d3.arc()
    .innerRadius(0)
    .outerRadius(radius - 10)

  const arcs = pieG.selectAll('.arc')
    .data(pie(pieData))
    .enter()
    .append('g')
    .attr('class', 'arc')

  arcs.append('path')
    .attr('d', arc)
    .attr('fill', d => d.data.color)
    .attr('stroke', 'white')
    .attr('stroke-width', 2)
    .style('cursor', 'pointer')
    .on('mouseover', function(event, d) {
      d3.select(this).attr('opacity', 0.8)
      const content = `<div class="tooltip-title">${d.data.type}</div>
        <div class="tooltip-row"><span class="tooltip-label">Count:</span> ${d.data.count}</div>`
      tooltip.innerHTML = content
      tooltip.classList.add('visible')
      tooltip.style.left = (event.pageX + 15) + 'px'
      tooltip.style.top = (event.pageY + 15) + 'px'
    })
    .on('mousemove', function(event) {
      tooltip.style.left = (event.pageX + 15) + 'px'
      tooltip.style.top = (event.pageY + 15) + 'px'
    })
    .on('mouseout', function() {
      d3.select(this).attr('opacity', 1)
      tooltip.classList.remove('visible')
    })
}
