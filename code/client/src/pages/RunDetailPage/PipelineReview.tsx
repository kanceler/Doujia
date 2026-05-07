import { useState } from 'react';
import type {
  PipelineHistoryNode,
  PipelineHistoryEdge,
  PipelineNodeStatus,
  PipelineNodeType,
  PipelineNodeDetail,
} from '@shared/api.interface';
import { Card, CardContent, CardHeader, CardTitle } from '@client/src/components/ui/card';
import { Badge } from '@client/src/components/ui/badge';
import { getPipelineNodeDetail } from '@client/src/api/pipeline-node';
import {
  Play,
  CheckCircle2,
  XCircle,
  AlertCircle,
  Clock,
  Loader2,
  RotateCcw,
  MessageSquare,
  GitBranch,
  FileText,
  ChevronDown,
  ChevronRight,
} from 'lucide-react';

const STATUS_ICON: Record<PipelineNodeStatus, React.ReactNode> = {
  pending: <Clock className="size-3.5" />,
  running: <Loader2 className="size-3.5 animate-spin" />,
  success: <CheckCircle2 className="size-3.5" />,
  failed: <XCircle className="size-3.5" />,
  rejected: <AlertCircle className="size-3.5" />,
  recovered: <RotateCcw className="size-3.5" />,
};

const STATUS_LABEL: Record<PipelineNodeStatus, string> = {
  pending: '等待中',
  running: '运行中',
  success: '成功',
  failed: '失败',
  rejected: '已拒绝',
  recovered: '已恢复',
};

const TYPE_ICON: Record<PipelineNodeType, React.ReactNode> = {
  task: <Play className="size-3" />,
  approval: <MessageSquare className="size-3" />,
  'sub-pipeline': <GitBranch className="size-3" />,
};

const STATUS_BORDER: Record<PipelineNodeStatus, string> = {
  pending: 'border-status-pending',
  running: 'border-status-running',
  success: 'border-status-success',
  failed: 'border-status-failed',
  rejected: 'border-status-rejected',
  recovered: 'border-status-recovered',
};

const STATUS_BG: Record<PipelineNodeStatus, string> = {
  pending: 'bg-status-pending/10',
  running: 'bg-status-running/10',
  success: 'bg-status-success/10',
  failed: 'bg-status-failed/10',
  rejected: 'bg-status-rejected/10',
  recovered: 'bg-status-recovered/10',
};

interface PipelineReviewProps {
  nodes: PipelineHistoryNode[];
  edges: PipelineHistoryEdge[];
}

const PipelineReview: React.FC<PipelineReviewProps> = ({ nodes, edges }) => {
  const [selectedNodeId, setSelectedNodeId] = useState<string | null>(null);
  const [nodeDetail, setNodeDetail] = useState<PipelineNodeDetail | null>(null);
  const [loadingDetail, setLoadingDetail] = useState(false);
  const [expandedSections, setExpandedSections] = useState<Record<string, boolean>>({});

  const sortedNodes = [...nodes].sort((a, b) => a.position - b.position);

  const buildAdjacency = (): Map<string, string[]> => {
    const adj = new Map<string, string[]>();
    for (const edge of edges) {
      if (!adj.has(edge.from)) adj.set(edge.from, []);
      adj.get(edge.from)!.push(edge.to);
    }
    return adj;
  };

  const adjacency = buildAdjacency();

  const findRoots = (): string[] => {
    const hasParent = new Set<string>();
    for (const edge of edges) hasParent.add(edge.to);
    return sortedNodes.filter((n) => !hasParent.has(n.id)).map((n) => n.id);
  };

  const roots = findRoots();

  const toggleSection = (key: string) => {
    setExpandedSections((prev) => ({ ...prev, [key]: !prev[key] }));
  };

  const handleNodeClick = async (nodeId: string) => {
    if (selectedNodeId === nodeId) {
      setSelectedNodeId(null);
      setNodeDetail(null);
      return;
    }
    setSelectedNodeId(nodeId);
    setLoadingDetail(true);
    try {
      const detail = await getPipelineNodeDetail(nodeId);
      setNodeDetail(detail);
    } catch {
      setNodeDetail(null);
    } finally {
      setLoadingDetail(false);
    }
  };

  const renderNode = (nodeId: string, depth: number = 0) => {
    const node = sortedNodes.find((n) => n.id === nodeId);
    if (!node) return null;

    const isSelected = selectedNodeId === nodeId;
    const children = adjacency.get(nodeId) || [];

    return (
      <div key={nodeId}>
        {depth > 0 && (
          <div
            className="ml-6 w-px h-4 bg-border"
            style={{ marginLeft: `${depth * 24 + 12}px` }}
          />
        )}
        <div
          className={`flex items-center gap-3 p-3 rounded-lg border-2 cursor-pointer transition-all duration-150 ${
            isSelected
              ? `${STATUS_BORDER[node.status]} ${STATUS_BG[node.status]} shadow-sm`
              : 'border-border hover:border-accent hover:bg-accent/30'
          }`}
          style={{ marginLeft: `${depth * 24}px` }}
          onClick={() => handleNodeClick(nodeId)}
          role="button"
          tabIndex={0}
          onKeyDown={(e) => e.key === 'Enter' && handleNodeClick(nodeId)}
        >
          <div className={`p-1.5 rounded-full ${STATUS_BG[node.status]}`}>
            {STATUS_ICON[node.status]}
          </div>
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2">
              {TYPE_ICON[node.type]}
              <span className="text-sm font-medium truncate">{node.name}</span>
            </div>
            {node.ref && (
              <span className="font-mono text-xs text-muted-foreground truncate block">
                {node.ref.slice(0, 7)}
              </span>
            )}
          </div>
          <Badge variant="outline" className="text-xs shrink-0">
            {STATUS_LABEL[node.status]}
          </Badge>
        </div>

        {isSelected && nodeDetail && (
          <div className="mt-2 ml-8 p-4 bg-card border border-border rounded-lg text-sm space-y-3">
            <div className="flex items-center justify-between">
              <span className="font-semibold">{nodeDetail.name}</span>
              <Badge variant="outline">{nodeDetail.type}</Badge>
            </div>

            {nodeDetail.snapshot && (
              <div>
                <button
                  className="flex items-center gap-1 text-xs font-medium text-muted-foreground hover:text-foreground"
                  onClick={() => toggleSection('snapshot')}
                >
                  {expandedSections['snapshot'] ? (
                    <ChevronDown className="size-3" />
                  ) : (
                    <ChevronRight className="size-3" />
                  )}
                  Snapshot
                </button>
                {expandedSections['snapshot'] && (
                  <pre className="mt-1 p-2 bg-muted rounded text-xs overflow-auto max-h-40 font-mono">
                    {JSON.stringify(nodeDetail.snapshot, null, 2)}
                  </pre>
                )}
              </div>
            )}

            {nodeDetail.artifact && (
              <div>
                <button
                  className="flex items-center gap-1 text-xs font-medium text-muted-foreground hover:text-foreground"
                  onClick={() => toggleSection('artifact')}
                >
                  {expandedSections['artifact'] ? (
                    <ChevronDown className="size-3" />
                  ) : (
                    <ChevronRight className="size-3" />
                  )}
                  Artifact
                </button>
                {expandedSections['artifact'] && (
                  <div className="mt-1 p-2 bg-muted rounded text-xs font-mono">
                    <div>Type: {nodeDetail.artifact.type}</div>
                    <div>Path: {nodeDetail.artifact.path}</div>
                    {nodeDetail.artifact.description && (
                      <div className="text-muted-foreground mt-1">
                        {nodeDetail.artifact.description}
                      </div>
                    )}
                  </div>
                )}
              </div>
            )}

            {nodeDetail.ref && (
              <div className="text-xs">
                <span className="text-muted-foreground">Ref: </span>
                <span className="font-mono">{nodeDetail.ref}</span>
              </div>
            )}
          </div>
        )}

        {isSelected && loadingDetail && (
          <div className="mt-2 ml-8 p-4 bg-card border border-border rounded-lg text-sm text-muted-foreground flex items-center gap-2">
            <Loader2 className="size-4 animate-spin" />
            加载节点详情...
          </div>
        )}

        {children.map((childId) => renderNode(childId, depth + 1))}
      </div>
    );
  };

  if (sortedNodes.length === 0) {
    return (
      <Card>
        <CardHeader>
          <CardTitle className="text-lg">Pipeline 执行图</CardTitle>
        </CardHeader>
        <CardContent>
          <div className="text-center py-8 text-muted-foreground text-sm">
            暂无 Pipeline 节点数据
          </div>
        </CardContent>
      </Card>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-lg">Pipeline 执行图</CardTitle>
      </CardHeader>
      <CardContent>
        <div className="space-y-1">
          {roots.length > 0
            ? roots.map((rootId) => renderNode(rootId, 0))
            : sortedNodes.map((node) => renderNode(node.id, 0))}
        </div>
      </CardContent>
    </Card>
  );
};

export default PipelineReview;
