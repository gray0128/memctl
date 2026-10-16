import { describe, expect, it } from 'vitest'
import { buildGraphElements, summarizeGraph } from './graph.js'

// This file verifies the pure data assembly of the read-only knowledge-graph
// view: reachable relations, orphan files and dead links from the snapshot DTO.

const graph = {
  nodes: [
    { path: 'MEMORY.md', kind: 'entrypoint', entryCount: 2, orphan: false },
    { path: 'alpha.md', kind: 'markdown', entryCount: 3, orphan: false },
    { path: 'orphan.md', kind: 'markdown', entryCount: 1, orphan: true },
  ],
  edges: [
    { from: 'MEMORY.md', to: 'alpha.md', exists: true },
    { from: 'alpha.md', to: 'missing-target', exists: false },
    // An ambiguous target keeps exists true but has no resolved path.
    { from: 'alpha.md', to: 'duplicate', exists: true },
  ],
}

describe('buildGraphElements', () => {
  it('maps managed files to nodes and links to edges', () => {
    const { nodes, edges } = buildGraphElements(graph)

    expect(nodes.map((node) => node.data.id)).toEqual([
      'MEMORY.md',
      'alpha.md',
      'orphan.md',
      'missing:missing-target',
      'missing:duplicate',
    ])
    expect(nodes.find((node) => node.data.id === 'orphan.md').classes).toBe('orphan')
    expect(edges.map((edge) => edge.data.source)).toEqual(['MEMORY.md', 'alpha.md', 'alpha.md'])
    expect(edges[0].classes).toBe('resolved')
    expect(edges[1].classes).toBe('dead')
  })

  it('summarizes reachable files, orphans, dead links and ambiguous targets', () => {
    expect(summarizeGraph(graph)).toEqual({
      files: 3,
      relations: 3,
      orphans: 1,
      deadLinks: 1,
      ambiguousLinks: 1,
    })
  })

  it('tolerates an empty graph', () => {
    expect(buildGraphElements(null)).toEqual({
      nodes: [],
      edges: [],
      summary: { files: 0, relations: 0, orphans: 0, deadLinks: 0, ambiguousLinks: 0 },
    })
  })
})
