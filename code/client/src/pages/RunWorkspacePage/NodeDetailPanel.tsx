import { useState } from 'react';
import type {
  DevflowArtifactContentView,
  DevflowArtifactRecord,
  DevflowNodeDetailView,
} from '@shared/devflow-api';
import { getDevflowArtifactContent } from '@/api/devflow-client';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Separator } from '@/components/ui/separator';
import { ClipboardCheck, Clock, FileCode, GitBranch } from 'lucide-react';

interface NodeDetailPanelProps {
  node: DevflowNodeDetailView | null;
}

export const NodeDetailPanel: React.FC<NodeDetailPanelProps> = ({ node }) => {
  const [artifactContent, setArtifactContent] = useState<Record<string, DevflowArtifactContentView>>({});
  const [artifactErrors, setArtifactErrors] = useState<Record<string, string>>({});
  const [loadingArtifactId, setLoadingArtifactId] = useState<string | null>(null);

  if (!node) {
    return (
      <div className="p-4 text-center text-sm text-muted-foreground">
        Click a pipeline node to inspect details
      </div>
    );
  }

  const snapshot = node.snapshot;
  const outputBags = snapshot?.output_bags ?? [];
  const inputBags = snapshot?.input_bags ?? [];

  const handleOpenArtifact = async (artifact: DevflowArtifactRecord) => {
    const id = artifactId(artifact);
    if (!id) return;
    setLoadingArtifactId(id);
    setArtifactErrors((current) => ({ ...current, [id]: '' }));
    try {
      const content = await getDevflowArtifactContent(node.task.run_id, id);
      setArtifactContent((current) => ({ ...current, [id]: content }));
    } catch (err) {
      setArtifactErrors((current) => ({
        ...current,
        [id]: err instanceof Error ? err.message : 'Failed to load artifact',
      }));
    } finally {
      setLoadingArtifactId(null);
    }
  };

  return (
    <ScrollArea className="max-h-[360px]">
      <div className="p-4 space-y-4">
        <div>
          <h4 className="text-sm font-semibold text-foreground">
            {node.task.stage_id || node.task.task_id}
          </h4>
          <div className="flex items-center gap-2 mt-1">
            <Badge variant="outline" className="text-xs">
              {node.task.agent_role}/{node.task.agent_id}
            </Badge>
            <StatusBadge status={node.task.status} />
            {node.task.result && <StatusBadge status={node.task.result} />}
          </div>
        </div>

        <Separator />

        {snapshot && (
          <div>
            <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground mb-1">
              <GitBranch className="size-3.5" />
              <span>Snapshot</span>
            </div>
            <code className="block px-2 py-1 bg-accent rounded text-xs font-mono text-accent-foreground border border-[hsl(142_72%_35%_0.2)] break-all">
              {snapshot.snapshot_id}
            </code>
          </div>
        )}

        {snapshot && (
          <div>
            <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground mb-1">
              <FileCode className="size-3.5" />
              <span>DoujiaGit Bags</span>
            </div>
            <div className="space-y-2">
              <BagList title="Input" bagIds={snapshot.input_bag_ids ?? []} />
              <BagList title="Output" bagIds={snapshot.output_bag_ids ?? []} />
              {(inputBags.length > 0 || outputBags.length > 0) && (
                <div className="px-2 py-1.5 bg-muted/50 rounded text-xs">
                  Versions: {[...inputBags, ...outputBags].reduce((sum, bag) => sum + (bag.versions?.length ?? 0), 0)}
                </div>
              )}
            </div>
          </div>
        )}

        {node.artifacts.length > 0 && (
          <div>
            <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground mb-1">
              <ClipboardCheck className="size-3.5" />
              <span>Artifacts</span>
            </div>
            <div className="space-y-1">
              {node.artifacts.map((artifact) => {
                const id = artifactId(artifact);
                return (
                  <ArtifactRow
                    key={id || artifactUri(artifact)}
                    artifact={artifact}
                    content={id ? artifactContent[id] : undefined}
                    error={id ? artifactErrors[id] : undefined}
                    loading={loadingArtifactId === id}
                    onOpen={() => handleOpenArtifact(artifact)}
                  />
                );
              })}
            </div>
          </div>
        )}

        {node.events.length > 0 && (
          <div>
            <div className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground mb-1">
              <Clock className="size-3.5" />
              <span>Events</span>
            </div>
            <div className="space-y-1">
              {node.events.map((event) => (
                <div key={event.event_id} className="px-2 py-1.5 bg-muted/50 rounded text-xs">
                  <div className="font-medium">{event.type}</div>
                  <div className="text-muted-foreground">{event.message}</div>
                </div>
              ))}
            </div>
          </div>
        )}

        <div className="text-xs text-muted-foreground">
          Updated {formatDateTime(node.task.updated_at)}
        </div>
      </div>
    </ScrollArea>
  );
};

const BagList: React.FC<{ title: string; bagIds: string[] }> = ({ title, bagIds }) => (
  <div className="px-2 py-1.5 bg-muted/50 rounded text-xs">
    <div className="text-muted-foreground mb-1">{title}</div>
    {bagIds.length > 0 ? (
      <div className="space-y-0.5">
        {bagIds.map((bagId) => (
          <div key={bagId} className="font-mono break-all">{bagId}</div>
        ))}
      </div>
    ) : (
      <div className="text-muted-foreground">None</div>
    )}
  </div>
);

const ArtifactRow: React.FC<{
  artifact: DevflowArtifactRecord;
  content?: DevflowArtifactContentView;
  error?: string;
  loading: boolean;
  onOpen: () => void;
}> = ({ artifact, content, error, loading, onOpen }) => (
  <div className="px-2 py-1.5 bg-muted/50 rounded text-xs space-y-1">
    <div className="flex items-center justify-between gap-2">
      <div>
        Type: <span className="font-mono">{artifact.kind ?? artifact.Kind ?? 'unknown'}</span>
      </div>
      <Button variant="outline" size="sm" className="h-6 px-2 text-xs" onClick={onOpen} disabled={loading || !artifactId(artifact)}>
        {loading ? 'Loading' : 'Open'}
      </Button>
    </div>
    <div>
      URI: <span className="font-mono break-all">{artifactUri(artifact) || '-'}</span>
    </div>
    {error && <div className="text-destructive">{error}</div>}
    {content?.text && (
      <pre className="max-h-32 overflow-auto whitespace-pre-wrap rounded bg-background p-2 text-[11px] border border-border">
        {content.text}
        {content.truncated ? '\n... truncated' : ''}
      </pre>
    )}
  </div>
);

function artifactId(artifact: DevflowArtifactRecord): string {
  return artifact.id ?? artifact.ID ?? '';
}

function artifactUri(artifact: DevflowArtifactRecord): string {
  return artifact.uri ?? artifact.URI ?? '';
}

const statusColorMap: Record<string, string> = {
  pending: 'bg-amber-100 text-amber-700 border-amber-200',
  waiting_external: 'bg-amber-100 text-amber-700 border-amber-200',
  dispatched: 'bg-blue-100 text-blue-700 border-blue-200',
  running: 'bg-blue-100 text-blue-700 border-blue-200',
  done: 'bg-green-100 text-green-700 border-green-200',
  kok: 'bg-green-100 text-green-700 border-green-200',
  kbug: 'bg-purple-100 text-purple-700 border-purple-200',
  failed: 'bg-red-100 text-red-700 border-red-200',
  kfail: 'bg-red-100 text-red-700 border-red-200',
  blocked: 'bg-red-100 text-red-600 border-red-200',
  krewrite: 'bg-red-100 text-red-600 border-red-200',
  kreplan: 'bg-red-100 text-red-600 border-red-200',
};

const StatusBadge: React.FC<{ status: string }> = ({ status }) => {
  const colors = statusColorMap[status] || 'bg-muted text-muted-foreground';
  return (
    <span className={`inline-flex items-center px-1.5 py-0.5 rounded text-[10px] font-medium border ${colors}`}>
      {status}
    </span>
  );
};

function formatDateTime(isoString: string): string {
  const date = new Date(isoString);
  return date.toLocaleString('zh-CN', {
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    minute: '2-digit',
  });
}
