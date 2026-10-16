import { useEffect, useMemo, useRef, useState } from 'preact/hooks'
import { buildGraphElements } from '../graph.js'

// KnowledgeGraph renders the read-only MEMORY.md reachability graph from the
// snapshot. Cytoscape draws the relations; the legend and counters cover dead
// links and orphan files. Node styles use Cytoscape's attribute selectors so
// the entry point, reachable files, orphan files and missing targets stay
// distinguishable without a second data pass.

const STYLE = [
  {
    selector: 'node',
    style: {
      label: 'data(label)',
      'font-size': 10,
      color: '#334155',
      'text-valign': 'bottom',
      'text-margin-y': 4,
      width: 18,
      height: 18,
      'background-color': '#94a3b8',
    },
  },
  { selector: 'node.reachable', style: { 'background-color': '#10b981' } },
  { selector: 'node.orphan', style: { 'background-color': '#f59e0b' } },
  { selector: 'node.missing', style: { 'background-color': '#ef4444', shape: 'diamond', width: 14, height: 14 } },
  { selector: 'node[kind = "entrypoint"]', style: { 'background-color': '#4f46e5', width: 26, height: 26 } },
  {
    selector: 'edge',
    style: {
      width: 1,
      'line-color': '#cbd5e1',
      'target-arrow-color': '#cbd5e1',
      'target-arrow-shape': 'triangle',
      'curve-style': 'bezier',
    },
  },
  {
    selector: 'edge.dead',
    style: { 'line-color': '#ef4444', 'target-arrow-color': '#ef4444', 'line-style': 'dashed' },
  },
]

function LegendItem({ color, shape, children }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span
        className={`inline-block ${shape === 'diamond' ? 'rotate-45' : 'rounded-full'}`}
        style={{ width: '0.7rem', height: '0.7rem', backgroundColor: color }}
      />
      {children}
    </span>
  )
}

export function KnowledgeGraph({ snapshot }) {
  const container = useRef(null)
  const [selected, setSelected] = useState(null)
  const graph = snapshot ? snapshot.graph : null
  // Rebuild the elements and the Cytoscape instance only when the graph content
  // changes. The SSE stream replaces snapshot.graph with a new object even when
  // the content is identical, so keying on object identity would reset pan,
  // zoom and selection on every refresh.
  const signature = useMemo(
    () => JSON.stringify(graph || { nodes: [], edges: [] }),
    [graph],
  )
  const built = useMemo(() => buildGraphElements(graph || { nodes: [], edges: [] }), [signature])

  useEffect(() => {
    if (!container.current || built.nodes.length === 0) return undefined
    let disposed = false
    let cy = null
    // Cytoscape is loaded on demand so the graph library stays out of the
    // initial bundle; the view only runs once the user opens it.
    import('cytoscape').then(({ default: cytoscape }) => {
      if (disposed || !container.current) return
      cy = cytoscape({
        container: container.current,
        elements: [...built.nodes, ...built.edges],
        style: STYLE,
        wheelSensitivity: 0.2,
        layout: { name: 'cose', animate: false, fit: true, padding: 24 },
      })
      cy.on('tap', 'node', (event) => {
        const node = event.target.data()
        setSelected({ id: node.id, label: node.label, kind: node.kind, entryCount: node.entryCount, orphan: node.orphan })
      })
      cy.on('tap', (event) => {
        if (event.target === cy) setSelected(null)
      })
    })
    return () => {
      disposed = true
      if (cy) cy.destroy()
    }
  }, [signature])

  if (!snapshot) {
    return <p className="text-sm text-slate-500 dark:text-slate-400">正在加载知识图谱…</p>
  }

  const { summary } = built
  return (
    <section className="space-y-4">
      <div className="flex flex-wrap items-baseline justify-between gap-2">
        <h1 className="text-xl font-semibold">知识图谱</h1>
        <span className="text-sm text-slate-500 dark:text-slate-400">
          入口 {snapshot.entryPoint || 'MEMORY.md'} · 文件 {summary.files} · 关系 {summary.relations}
        </span>
      </div>
      <div
        ref={container}
        className="relative h-96 w-full rounded-lg border border-slate-200 bg-white dark:border-slate-800 dark:bg-slate-900"
      />
      <div className="flex flex-wrap gap-4 text-xs text-slate-600 dark:text-slate-300">
        <LegendItem color="#4f46e5">入口</LegendItem>
        <LegendItem color="#10b981">可达</LegendItem>
        <LegendItem color="#f59e0b">孤立文件 {summary.orphans}</LegendItem>
        <LegendItem color="#ef4444">死链 {summary.deadLinks}</LegendItem>
        <LegendItem color="#ef4444" shape="diamond">
          未解析目标 {summary.ambiguousLinks}
        </LegendItem>
      </div>
      {selected && (
        <div className="rounded-lg border border-slate-200 bg-white p-3 text-sm dark:border-slate-800 dark:bg-slate-900">
          <div className="font-medium break-all">{selected.label}</div>
          <div className="mt-1 text-slate-500 dark:text-slate-400">
            类型 {selected.kind} · 条目 {selected.entryCount}
            {selected.orphan ? ' · 孤立文件' : ''}
          </div>
        </div>
      )}
    </section>
  )
}
