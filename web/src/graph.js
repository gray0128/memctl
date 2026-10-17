// Knowledge-graph assembly for the read-only Cytoscape view.
//
// The snapshot already resolved reachable relations, orphans and dead links in
// docs/02-domain-models-and-contracts.md section 2.1. This module only turns
// that data into Cytoscape elements and a summary; it holds no rendering state
// so it can be unit tested without a DOM or canvas.

// graphLabel returns the node label shown in the graph. The full path stays on
// the node data and in the selection panel; a long path in every label makes
// adjacent nodes unreadable on a dense graph.
export function graphLabel(path) {
  const segments = String(path || '').split('/')
  return segments[segments.length - 1]
}

// buildGraphElements returns Cytoscape node and edge collections plus a
// summary. A link edge whose target is not a managed file (a dead link or an
// ambiguous one) gets a synthetic missing node, so the edge stays visible
// instead of being dropped by Cytoscape.
export function buildGraphElements(graph) {
  const nodes = (graph && graph.nodes ? graph.nodes : []).map((node) => ({
    data: {
      id: node.path,
      label: graphLabel(node.path),
      fullPath: node.path,
      kind: node.kind,
      entryCount: node.entryCount || 0,
      orphan: !!node.orphan,
    },
    classes: node.orphan ? 'orphan' : 'reachable',
  }))
  const known = new Set(nodes.map((node) => node.data.id))
  const synthetic = new Map()
  const edges = []

  ;(graph && graph.edges ? graph.edges : []).forEach((edge, index) => {
    let target = edge.to
    if (!known.has(target)) {
      if (!synthetic.has(target)) {
        const id = `missing:${target}`
        synthetic.set(target, id)
        nodes.push({
          data: { id, label: graphLabel(target), fullPath: target, kind: 'missing', entryCount: 0, orphan: false, missing: true },
          classes: 'missing',
        })
      }
      target = synthetic.get(target)
    }
    edges.push({
      data: { id: `edge-${index}`, source: edge.from, target, exists: !!edge.exists },
      classes: edge.exists ? 'resolved' : 'dead',
    })
  })

  return { nodes, edges, summary: summarizeGraph(graph) }
}

// summarizeGraph counts the three findings the view must show: orphan files,
// dead links and ambiguous targets. It keeps them separate instead of
// collapsing them into one health score.
export function summarizeGraph(graph) {
  const nodes = graph && graph.nodes ? graph.nodes : []
  const edges = graph && graph.edges ? graph.edges : []
  const paths = new Set(nodes.map((node) => node.path))
  const deadLinks = edges.filter((edge) => !edge.exists).length
  const ambiguousLinks = edges.filter((edge) => edge.exists && !paths.has(edge.to)).length
  const orphans = nodes.filter((node) => node.orphan).length
  return {
    files: nodes.length,
    relations: edges.length,
    orphans,
    deadLinks,
    ambiguousLinks,
  }
}
