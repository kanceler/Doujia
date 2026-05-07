import { Blocks, Clock3, ExternalLink, GitCommitHorizontal, PackageOpen } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { PIPELINE_STATUS_BADGE_CLASS, PIPELINE_STATUS_LABEL, formatDuration } from './pipeline-status-style';
import type { PipelineNodeActionHandlers, PipelineTaskNode } from './pipeline-types';

interface PipelineDetailsPanelProps extends PipelineNodeActionHandlers {
  node: PipelineTaskNode | null;
}

export const PipelineDetailsPanel: React.FC<PipelineDetailsPanelProps> = ({
  node,
  onOpenPipeline,
  onOpenTaskSnapshots,
}) => {
  if (!node) {
    return (
      <aside className="flex h-full w-[258px] shrink-0 flex-col border-l border-slate-200/70 bg-white/62 px-4 py-5 backdrop-blur">
        <div className="flex size-9 items-center justify-center rounded-[16px] bg-[radial-gradient(circle_at_top,#ffffff_0%,rgba(226,232,240,0.9)_45%,rgba(203,213,225,0.45)_100%)] text-slate-500 shadow-[0_8px_20px_rgba(15,23,42,0.06)]">
          <Blocks className="size-4" />
        </div>
        <div className="mt-4 text-sm font-semibold text-slate-900">选择一个节点</div>
        <p className="mt-2 text-sm leading-6 text-slate-500">
          点击上方 pipeline 里的任务节点，这里会展示输入输出、状态和快照入口。
        </p>
      </aside>
    );
  }

  return (
    <aside className="flex h-full w-[258px] shrink-0 flex-col border-l border-slate-200/70 bg-white/62 backdrop-blur">
      <div className="border-b border-slate-200/70 px-4 py-4">
        <div className="flex items-start justify-between gap-3">
          <div className="min-w-0">
            <div className="truncate text-sm font-semibold text-slate-950">{node.title}</div>
            <div className="mt-1 truncate text-xs text-slate-500">{node.stageId || `${node.role}_${node.op}`}</div>
          </div>
          <span className={`shrink-0 rounded-full border px-2 py-1 text-[11px] font-semibold ${PIPELINE_STATUS_BADGE_CLASS[node.status]}`}>
            {PIPELINE_STATUS_LABEL[node.status]}
          </span>
        </div>
      </div>

      <div className="min-h-0 flex-1 overflow-y-auto px-4 py-4">
        <dl className="space-y-3 text-xs">
          <DetailRow label="Role" value={node.role} />
          <DetailRow label="Op" value={node.op} />
          <DetailRow label="Status" value={node.rawStatus || node.status} />
          <DetailRow label="Duration" value={formatDuration(node.durationMs)} icon={<Clock3 className="size-3.5" />} />
        </dl>

        <ArtifactSection title="输入产物" items={node.inputArtifacts ?? []} />
        <ArtifactSection title="输出产物" items={node.outputArtifacts ?? []} />

        {node.errorReason ? (
          <div className="mt-4 rounded-[16px] border border-orange-200/80 bg-orange-50/90 px-3 py-2 text-xs leading-5 text-orange-800">
            {node.errorReason}
          </div>
        ) : null}
      </div>

      <div className="space-y-2 border-t border-slate-200/70 px-4 py-3">
        {node.childPipelineInstanceId ? (
          <Button
            type="button"
            variant="outline"
            className="h-8 w-full justify-start gap-2 rounded-full border-slate-200 bg-white text-xs"
            onClick={() => onOpenPipeline?.(node.childPipelineInstanceId!, node.title)}
          >
            <ExternalLink className="size-4" />
            打开子 pipeline
          </Button>
        ) : null}
        <Button
          type="button"
          variant="outline"
          className="h-8 w-full justify-start gap-2 rounded-full border-slate-200 bg-white text-xs"
          disabled={!node.taskId}
          onClick={() => node.taskId && onOpenTaskSnapshots?.(node.taskId, node.title)}
        >
          <GitCommitHorizontal className="size-4" />
          打开 snapshots
        </Button>
      </div>
    </aside>
  );
};

const DetailRow: React.FC<{ label: string; value: string; icon?: React.ReactNode }> = ({ label, value, icon }) => (
  <div className="flex items-center justify-between gap-3">
    <dt className="text-[10px] font-medium uppercase tracking-wide text-slate-400">{label}</dt>
    <dd className="inline-flex min-w-0 items-center gap-1.5 truncate font-medium text-slate-700">
      {icon}
      <span className="truncate">{value || '--'}</span>
    </dd>
  </div>
);

const ArtifactSection: React.FC<{ title: string; items: string[] }> = ({ title, items }) => (
  <section className="mt-5">
    <div className="mb-2 flex items-center gap-2 text-[10px] font-semibold uppercase tracking-wide text-slate-400">
      <PackageOpen className="size-3.5" />
      {title}
    </div>
    {items.length > 0 ? (
      <div className="space-y-2">
        {items.map((item, index) => (
          <div key={`${item}-${index}`} className="rounded-[14px] border border-slate-200/80 bg-slate-50/80 px-3 py-2 text-xs text-slate-600">
            {item}
          </div>
        ))}
      </div>
    ) : (
      <div className="rounded-[14px] border border-dashed border-slate-200 bg-slate-50/70 px-3 py-2 text-xs text-slate-400">
        暂无数据
      </div>
    )}
  </section>
);
