import { useEffect, useMemo, useRef, useState } from 'preact/hooks'
import { buildGraphElements } from '../graph.js'
import { SplitPanel } from './SplitPanel.jsx'

// KnowledgeGraph renders the MEMORY.md reachability graph from the snapshot.
// Cytoscape draws the relations; the legend and counters cover dead links and
// orphan files. Node styles use Cytoscape's attribute selectors so the entry
// point, reachable files, orphan files and missing targets stay distinguishable
// without a second data pass. Selecting a file also opens the split action that
// moves one of its entries into a new file.

const STYLE = [
  {
    selector: 'node',
    style: {
      label: 'data(label)',
      'font-size': 11,
      'font-family': 'ui-sans-serif, system-ui, sans-serif',
      'font-weight': 500,
      color: '#475569',
      'text-valign': 'bottom',
      'text-margin-y': 5,
      // Long file names are truncated instead of growing into a wide line
      // that overlaps the neighbouring label; the selection panel keeps the
      // full path.
      'text-wrap': 'ellipsis',
      'text-max-width': 120,
      'text-justification': 'center',
      width: 20,
      height: 20,
      'background-color': '#94a3b8',
      'border-width': 2,
      'border-color': '#ffffff',
      'transition-property': 'background-color, border-color, width, height',
      'transition-duration': '0.2s',
    },
  },
  {
    selector: 'node.reachable',
    style: {
      'background-color': '#10b981',
      'border-color': '#d1fae5',
    },
  },
  {
    selector: 'node.orphan',
    style: {
      'background-color': '#f59e0b',
      'border-color': '#fef3c7',
    },
  },
  {
    selector: 'node.missing',
    style: {
      'background-color': '#ef4444',
      'border-color': '#fee2e2',
      shape: 'diamond',
      width: 16,
      height: 16,
    },
  },
  {
    selector: 'node[kind = "entrypoint"]',
    style: {
      'background-color': '#6366f1',
      'border-color': '#e0e7ff',
      'border-width': 3,
      width: 28,
      height: 28,
      'font-weight': 700,
      color: '#4338ca',
    },
  },
  {
    selector: 'node:selected',
    style: {
      'border-color': '#1e1b4b',
      'border-width': 3,
      width: 24,
      height: 24,
    },
  },
  {
    selector: 'edge',
    style: {
      width: 1.5,
      'line-color': '#cbd5e1',
      'target-arrow-color': '#cbd5e1',
      'target-arrow-shape': 'triangle',
      'arrow-scale': 0.8,
      'curve-style': 'bezier',
    },
  },
  {
    selector: 'edge.dead',
    style: {
      'line-color': '#ef4444',
      'target-arrow-color': '#ef4444',
      'line-style': 'dashed',
      'line-dash-pattern': [4, 3],
    },
  },
]

function LegendBadge({ color, shape, count = null, children }) {
  return (
    <div className="inline-flex items-center gap-1.5 rounded-md border border-slate-200/60 bg-white/80 px-2.5 py-1 text-xs font-medium text-slate-700 shadow-2xs backdrop-blur-xs dark:border-slate-800/80 dark:bg-slate-900/80 dark:text-slate-300">
      <span
        className={`inline-block ${shape === 'diamond' ? 'rotate-45' : 'rounded-full'}`}
        style={{ width: '0.6rem', height: '0.6rem', backgroundColor: color }}
      />
      <span>{children}</span>
      {count !== null && (
        <span className="ml-1 rounded-full bg-slate-100 px-1.5 py-0.2 font-mono text-[10px] text-slate-600 dark:bg-slate-800 dark:text-slate-400">
          {count}
        </span>
      )}
    </div>
  )
}

export function KnowledgeGraph({ api, snapshot, refreshKey }) {
  const container = useRef(null)
  const cyInstance = useRef(null)
  const [selected, setSelected] = useState(null)
  const graph = snapshot ? snapshot.graph : null

  const signature = useMemo(
    () => JSON.stringify(graph || { nodes: [], edges: [] }),
    [graph],
  )
  const built = useMemo(() => buildGraphElements(graph || { nodes: [], edges: [] }), [signature])

  useEffect(() => {
    if (!container.current || built.nodes.length === 0) return undefined
    let disposed = false

    import('cytoscape').then(({ default: cytoscape }) => {
      if (disposed || !container.current) return
      const cy = cytoscape({
        container: container.current,
        elements: [...built.nodes, ...built.edges],
        style: STYLE,
        wheelSensitivity: 0.25,
        layout: {
          name: 'cose',
          animate: false,
          fit: true,
          padding: 40,
          // Labels sit under their nodes and grow with the zoom, so the
          // layout spreads nodes further apart than the label width instead
          // of letting adjacent file names collide.
          nodeRepulsion: 400000,
          idealEdgeLength: 120,
          nodeOverlap: 120,
          componentSpacing: 200,
          gravity: 0.2,
        },
      })
      cyInstance.current = cy

      cy.on('tap', 'node', (event) => {
        const node = event.target.data()
        setSelected({
          id: node.id,
          label: node.fullPath || node.label,
          kind: node.kind,
          entryCount: node.entryCount,
          orphan: node.orphan,
        })
      })

      cy.on('tap', (event) => {
        if (event.target === cy) setSelected(null)
      })
    })

    return () => {
      disposed = true
      if (cyInstance.current) {
        cyInstance.current.destroy()
        cyInstance.current = null
      }
    }
  }, [signature])

  function handleFit() {
    if (cyInstance.current) {
      cyInstance.current.fit(undefined, 40)
    }
  }

  function handleZoomIn() {
    if (cyInstance.current) {
      cyInstance.current.zoom(cyInstance.current.zoom() * 1.25)
      cyInstance.current.center()
    }
  }

  function handleZoomOut() {
    if (cyInstance.current) {
      cyInstance.current.zoom(cyInstance.current.zoom() * 0.8)
      cyInstance.current.center()
    }
  }

  if (!snapshot) {
    return (
      <div className="rounded-2xl border border-slate-200/80 bg-white p-12 text-center shadow-2xs dark:border-slate-800/80 dark:bg-slate-900">
        <p className="text-sm text-slate-500 dark:text-slate-400">正在构建知识图谱拓扑…</p>
      </div>
    )
  }

  const { summary } = built

  return (
    <section className="space-y-5">
      {/* Header */}
      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-xl font-bold tracking-tight text-slate-900 dark:text-white">知识拓扑图谱</h1>
          <p className="mt-0.5 text-xs text-slate-500 dark:text-slate-400">
            MEMORY.md 索引链路、节点连通性与孤立检测
          </p>
        </div>
        <div className="flex items-center gap-2">
          <span className="rounded-md border border-slate-200/80 bg-white px-2.5 py-1 text-xs text-slate-600 shadow-2xs dark:border-slate-800/80 dark:bg-slate-900 dark:text-slate-300">
            入口 <code className="font-semibold text-indigo-600 dark:text-indigo-400">{snapshot.entryPoint || 'MEMORY.md'}</code>
          </span>
          <span className="rounded-md border border-slate-200/80 bg-white px-2.5 py-1 text-xs text-slate-600 shadow-2xs dark:border-slate-800/80 dark:bg-slate-900 dark:text-slate-300">
            文件 <strong className="text-slate-900 dark:text-white">{summary.files}</strong>
          </span>
          <span className="rounded-md border border-slate-200/80 bg-white px-2.5 py-1 text-xs text-slate-600 shadow-2xs dark:border-slate-800/80 dark:bg-slate-900 dark:text-slate-300">
            关系 <strong className="text-slate-900 dark:text-white">{summary.relations}</strong>
          </span>
        </div>
      </div>

      {/* Graph Viewport Card */}
      <div className="relative overflow-hidden rounded-2xl border border-slate-200/80 bg-white shadow-2xs dark:border-slate-800/80 dark:bg-slate-900">
        {/* Canvas */}
        <div
          ref={container}
          className="relative h-[26rem] w-full bg-dot-grid-light dark:bg-dot-grid-dark md:h-[34rem]"
        />

        {/* Floating Zoom Controls */}
        <div className="absolute right-4 bottom-4 flex flex-col items-center gap-1.5 rounded-xl border border-slate-200/80 bg-white/90 p-1.5 shadow-md backdrop-blur-md dark:border-slate-800/80 dark:bg-slate-900/90">
          <button
            type="button"
            onClick={handleZoomIn}
            title="放大画布"
            className="flex h-7 w-7 items-center justify-center rounded-lg text-slate-600 hover:bg-slate-100 hover:text-slate-900 active:scale-95 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-white"
          >
            <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2.2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M12 4v16m8-8H4" />
            </svg>
          </button>
          <button
            type="button"
            onClick={handleZoomOut}
            title="缩小画布"
            className="flex h-7 w-7 items-center justify-center rounded-lg text-slate-600 hover:bg-slate-100 hover:text-slate-900 active:scale-95 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-white"
          >
            <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2.2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M20 12H4" />
            </svg>
          </button>
          <div className="my-0.5 h-px w-4 bg-slate-200 dark:bg-slate-700" />
          <button
            type="button"
            onClick={handleFit}
            title="适应视口"
            className="flex h-7 w-7 items-center justify-center rounded-lg text-slate-600 hover:bg-slate-100 hover:text-slate-900 active:scale-95 dark:text-slate-400 dark:hover:bg-slate-800 dark:hover:text-white"
          >
            <svg className="h-4 w-4" fill="none" viewBox="0 0 24 24" stroke="currentColor" strokeWidth="2">
              <path strokeLinecap="round" strokeLinejoin="round" d="M4 8V4m0 0h4M4 4l5 5m11-5h-4m4 0v4m0-4l-5 5M4 16v4m0 0h4m-4 0l5-5m11 5l-5-5m5 5v-4m0 4h-4" />
            </svg>
          </button>
        </div>
      </div>

      {/* Legend Ribbon */}
      <div className="flex flex-wrap items-center gap-2">
        <LegendBadge color="#6366f1">入口文件</LegendBadge>
        <LegendBadge color="#10b981">可达节点</LegendBadge>
        <LegendBadge color="#f59e0b" count={summary.orphans}>孤立文件</LegendBadge>
        <LegendBadge color="#ef4444" count={summary.deadLinks}>死链目标</LegendBadge>
        <LegendBadge color="#ef4444" shape="diamond" count={summary.ambiguousLinks}>未解析/歧义目标</LegendBadge>
      </div>

      {/* Node Selected Inspector & Split Panel */}
      {selected && (
        <div className="rounded-xl border border-indigo-200/80 bg-white p-5 shadow-xs transition-all dark:border-indigo-900/60 dark:bg-slate-900">
          <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between border-b border-slate-200/70 pb-4 dark:border-slate-800/70">
            <div>
              <div className="flex items-center gap-2">
                <span className="text-xs font-semibold uppercase tracking-wider text-indigo-600 dark:text-indigo-400">已选中节点</span>
                <span className="rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-medium text-slate-600 dark:bg-slate-800 dark:text-slate-400">
                  {selected.kind}
                </span>
                {selected.orphan && (
                  <span className="rounded-full bg-amber-50 px-2 py-0.5 text-[10px] font-semibold text-amber-700 dark:bg-amber-950/40 dark:text-amber-300">
                    孤立文件
                  </span>
                )}
              </div>
              <h3 className="mt-1 font-mono text-sm font-semibold text-slate-900 dark:text-slate-100 break-all">
                {selected.label}
              </h3>
            </div>
            <div className="text-xs text-slate-500 dark:text-slate-400">
              包含条目数: <strong className="font-mono text-slate-800 dark:text-slate-200">{selected.entryCount}</strong>
            </div>
          </div>

          {selected.kind !== 'missing' && (
            <div className="mt-4">
              <SplitPanel api={api} sourcePath={selected.id} refreshKey={refreshKey} />
            </div>
          )}
        </div>
      )}
    </section>
  )
}
