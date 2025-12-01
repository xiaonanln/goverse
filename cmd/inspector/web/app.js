// Inspector Graph - Main Application Entry Point

// Tab switching
document.querySelectorAll('.tab').forEach(tab => {
  tab.addEventListener('click', () => {
    document.querySelectorAll('.tab').forEach(t => t.classList.remove('active'))
    document.querySelectorAll('.view').forEach(v => v.classList.remove('active'))

    tab.classList.add('active')
    const viewId = tab.dataset.view + '-view'
    document.getElementById(viewId).classList.add('active')

    if (tab.dataset.view === 'shard') {
      updateShardView()
    } else if (tab.dataset.view === 'nodes') {
      updateNodesView()
    }
  })
})

// Window resize handler for both graph and nodes views
window.addEventListener('resize', () => {
  handleGraphResize()
  handleNodesViewResize()
})

// Initialize application
initGraph()
connectSSE()
