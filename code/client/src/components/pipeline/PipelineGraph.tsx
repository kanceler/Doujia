import { useCallback, useMemo, useState } from 'react';
import {
  Background,
  Controls,
  MarkerType,
  MiniMap,
  ReactFlow,
  type Edge,
  type Node,
  type NodeMouseHandler,
} from '@xyflow/react';
import '@xyflow/react/dist/style.css';
import { Check, CircleAlert, CircleDashed, Clock3, Loader2, Workflow } from 'lucide-react';
import { cn } from '@/lib/utils';
import { PipelineDetailsPanel } from './PipelineDetailsPanel';
import { PipelineNode } from './PipelineNode';
import { layoutPipelineGraph } from './pipeline-layout';
import {
  PIPELINE_EDGE_COLOR,
  PIPELINE_STATUS_BADGE_CLASS,
  PIPELINE_STATUS_LABEL,
  edgeStyleForStatus,
  formatDuration,
  summarizePipeline,
  summarizeStatus,
} from './pipeline-status-style';
import type {
  PipelineNodeActionHandlers,
  PipelineNodeViewData,
  PipelineTaskEdge,
  PipelineTaskNode,
} from './pipeline-types';

interface PipelineGraphProps extends PipelineNodeActionHandlers {
  nodes: PipelineTaskNode[];
  edges: PipelineTaskEdge[];
  selectedNodeId?: string | null;
  onNodeSelect?: (nodeId: string) => void;
  title?: string;
  iterationNo?: number;
  status?: string;
  emptyMessage?: string;
  className?: string;
}

const nodeTypes = {
  pipelineTask: PipelineNode,
};

export const PipelineGraph: React.FC<PipelineGraphProps> = ({
  nodes,
  edges,
  selectedNodeId,
  onNodeSelect,
  title = 'Main pipeline',
  iterationNo = 0,
  status = 'not_started',
  emptyMessage = 'Main pipeline not ready yet.',
  className,
  onOpenPipeline,
  onOpenTaskSnapshots,
}) => {
  const [internalSelectedNodeId, setInternalSelectedNodeId] = useState<string | null>(null);
  const activeNodeId = selectedNodeId ?? internalSelectedNodeId ?? null;
  const selectedNode = nodes.find((node) => node.id === activeNodeId) ?? null;
  const summary = useMemo(() => summarizePipeline(nodes), [nodes]);
  const normalizedRunStatus = useMemo(() => summarizeStatus(summary, status), [status, summary]);

  const handleSelect = useCallback((nodeId: string) => {
    setInternalSelectedNodeId(nodeId);
    onNodeSelect?.(nodeId);
  }, [onNodeSelect]);

  const layout = useMemo(() => layoutPipelineGraph(nodes, edges), [nodes, edges]);

  const flowNodes = useMemo<Node<PipelineNodeViewData>[]>(() => (
    layout.nodes.map((node) => ({
      id: node.id,
      type: 'pipelineTask',
      position: node.position,
      draggable: false,
      selectable: true,
      data: {
        node,
        selected: node.id === activeNodeId,
        onSelect: handleSelect,
        onOpenPipeline,
        onOpenTaskSnapshots,
      },
    }))
  ), [activeNodeId, handleSelect, layout.nodes, onOpenPipeline, onOpenTaskSnapshots]);

  const flowEdges = useMemo<Edge[]>(() => (
    layout.edges.map((edge) => {
      const statusKey = edge.status ?? 'normal';
      return {
        id: edge.id,
        source: edge.source,
        target: edge.target,
        type: 'smoothstep',
        animated: statusKey === 'active',
        style: edgeStyleForStatus(statusKey),
        markerEnd: {
          type: MarkerType.ArrowClosed,
          color: PIPELINE_EDGE_COLOR[statusKey],
          width: 18,
          height: 18,
        },
      };
    })
  ), [layout.edges]);

  const onFlowNodeClick = useCallback<NodeMouseHandler>((_, node) => {
    handleSelect(node.id);
  }, [handleSelect]);

  return (
    <section className={cn('flex h-full min-h-[220px] flex-col overflow-hidden rounded-xl bg-white', className)}>
      <header className="flex shrink-0 items-center justify-between border-b border-slate-100 bg-white px-5 py-3.5">
        <div className="flex min-w-0 items-center gap-3">
          <div className="flex size-9 shrink-0 items-center justify-center rounded-full bg-blue-50 text-blue-700">
            <Workflow className="size-5" />
          </div>
          <div className="min-w-0">
            <h2 className="truncate text-base font-semibold text-slate-950">{title}</h2>
            <div className="mt-0.5 text-xs text-slate-500">Iteration {iterationNo}</div>
          </div>
        </div>
        <div className="shrink-0 text-right">
          <div className={`inline-flex items-center gap-1.5 rounded-full border px-2.5 py-1 text-xs font-semibold ${PIPELINE_STATUS_BADGE_CLASS[normalizedRunStatus]}`}>
            {normalizedRunStatus === 'running' ? <Loader2 className="size-3.5 animate-spin" /> : <Check className="size-3.5" />}
            {PIPELINE_STATUS_LABEL[normalizedRunStatus]}
          </div>
          <div className="mt-1.5 text-xs text-slate-500">Total duration | {formatDuration(summary.totalDurationMs)}</div>
        </div>
      </header>

      <div className="flex min-h-0 flex-1">
        <div className="flex min-w-0 flex-1 flex-col bg-slate-50">
          <div className="relative min-h-0 flex-1">
            {nodes.length === 0 ? (
              <div className="absolute inset-0 flex items-center justify-center">
                <div className="rounded-lg border border-dashed border-slate-300 bg-white px-5 py-4 text-sm text-slate-500 shadow-sm">
                  {emptyMessage}
                </div>
              </div>
            ) : (
              <ReactFlow
                className="pipeline-react-flow"
                nodes={flowNodes}
                edges={flowEdges}
                nodeTypes={nodeTypes}
                onNodeClick={onFlowNodeClick}
                fitView
                fitViewOptions={{ padding: 0.16, maxZoom: 0.92, minZoom: 0.48 }}
                minZoom={0.48}
                maxZoom={1.35}
                nodesDraggable={false}
                nodesConnectable={false}
                elementsSelectable
                proOptions={{ hideAttribution: true }}
              >
              <Background color="#dbe3ee" gap={24} size={0.8} />
              <Controls showInteractive={false} position="bottom-left" className="!scale-90 !origin-bottom-left" />
              <MiniMap
                pannable
                zoomable
                position="bottom-right"
                nodeColor="#d9e8ff"
                maskColor="rgba(248, 250, 252, 0.76)"
                style={{ width: 118, height: 82 }}
                className="!border !border-slate-200/80 !bg-white/88 !shadow-[0_10px_24px_rgba(15,23,42,0.06)]"
              />
              </ReactFlow>
            )}
          </div>
          <PipelineStatsBar summary={summary} />
        </div>
        <PipelineDetailsPanel
          node={selectedNode}
          onOpenPipeline={onOpenPipeline}
          onOpenTaskSnapshots={onOpenTaskSnapshots}
        />
      </div>
    </section>
  );
};

const PipelineStatsBar: React.FC<{ summary: ReturnType<typeof summarizePipeline> }> = ({ summary }) => (
  <div className="shrink-0 border-t border-slate-100 bg-white px-3 py-2">
    <div className="grid grid-cols-[repeat(auto-fit,minmax(92px,1fr))] gap-2 text-xs">
      <StatItem label="Stages" value={summary.total} icon={<Workflow className="size-3.5 text-slate-500" />} />
      <StatItem label="Completed" value={summary.completed} icon={<Check className="size-3.5 text-emerald-600" />} />
      <StatItem label="Running" value={summary.running} icon={<Loader2 className={cn('size-3.5 text-blue-600', summary.running > 0 && 'animate-spin')} />} />
      <StatItem label="Failed" value={summary.failed + summary.kbug} icon={<CircleAlert className="size-3.5 text-orange-600" />} />
      <StatItem label="Skipped" value={summary.skipped} icon={<CircleDashed className="size-3.5 text-slate-500" />} />
      <StatItem label="Duration" value={formatDuration(summary.totalDurationMs)} icon={<Clock3 className="size-3.5 text-slate-500" />} />
    </div>
  </div>
);

const StatItem: React.FC<{ label: string; value: number | string; icon: React.ReactNode }> = ({ label, value, icon }) => (
  <div className="flex min-w-0 items-center justify-center gap-1.5">
    <span className="flex size-6 shrink-0 items-center justify-center rounded-full bg-slate-50">{icon}</span>
    <div className="min-w-0">
      <div className="truncate text-xs font-semibold text-slate-950">{value}</div>
      <div className="truncate text-[10px] text-slate-500">{label}</div>
    </div>
  </div>
);
