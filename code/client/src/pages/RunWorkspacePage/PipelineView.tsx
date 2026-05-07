import { useMemo, useState } from 'react';
import type { DevflowPipelineWorkspaceView } from '@shared/devflow-api';
import { PipelineGraph } from '@/components/pipeline/PipelineGraph';
import { toPipelineGraphData } from '@/components/pipeline/pipeline-adapter';

interface PipelineViewProps {
  workspace: DevflowPipelineWorkspaceView | null;
  title?: string;
  onOpenPipeline: (instanceId: string, title: string) => void;
  onOpenTaskSnapshots: (taskId: string, title: string) => void;
}

export const PipelineView: React.FC<PipelineViewProps> = ({
  workspace,
  title = 'Main pipeline',
  onOpenPipeline,
  onOpenTaskSnapshots,
}) => {
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const graph = useMemo(() => toPipelineGraphData(workspace), [workspace]);
  const graphTitle = title.trim() || 'Main pipeline';
  const hasBackendGraph = Boolean(
    workspace?.main_pipeline_nodes?.length || workspace?.main_pipeline_edges?.length,
  );

  return (
    <div className="h-full min-h-0 rounded-xl border border-slate-200 bg-white shadow-sm">
      <PipelineGraph
        key={graphTitle}
        nodes={graph.nodes}
        edges={graph.edges}
        selectedNodeId={selectedNodeId}
        onNodeSelect={setSelectedNodeId}
        title={graphTitle}
        iterationNo={workspace?.current_iteration_no ?? 0}
        status={workspace?.status ?? 'not_started'}
        emptyMessage={hasBackendGraph ? 'Main pipeline not ready yet.' : 'Main pipeline not ready yet.'}
        onOpenPipeline={onOpenPipeline}
        onOpenTaskSnapshots={onOpenTaskSnapshots}
      />
    </div>
  );
};
