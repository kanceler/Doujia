import type {
  DevflowGitBranchesView,
  DevflowPipelineWorkspaceView,
  DevflowWorkspaceOverviewView,
} from '@shared/devflow-api';
import { GitBranchesPanel } from './GitBranchesPanel';
import {
  buildPipelineOverviewItems,
  type OverviewFieldItem,
} from './pipeline-overview-items';

interface PipelineOverviewPanelProps {
  workspaceOverview?: DevflowWorkspaceOverviewView | null;
  pipelineWorkspace?: DevflowPipelineWorkspaceView | null;
  gitBranches?: DevflowGitBranchesView | null;
  gitBranchesLoading?: boolean;
  embedded?: boolean;
}

function statusClass(status?: string): string {
  if (status === 'failed') return 'border-red-200 bg-red-50 text-red-700';
  if (status === 'completed' || status === 'done' || status === 'success') {
    return 'border-emerald-200 bg-emerald-50 text-emerald-700';
  }
  if (status === 'running' || status === 'waiting_human' || status === 'dispatched') {
    return 'border-orange-200 bg-orange-50 text-orange-700';
  }
  return 'border-slate-200 bg-slate-50 text-slate-600';
}

function isStatusValue(value: string): boolean {
  return ['completed', 'done', 'success', 'failed', 'running', 'waiting_human', 'dispatched'].includes(
    value,
  );
}

function OverviewFieldRow({ item }: { item: OverviewFieldItem }) {
  return (
    <div className="grid grid-cols-[132px_minmax(0,1fr)] items-start gap-3 border-b border-slate-100 py-3 last:border-b-0">
      <div className="text-xs font-semibold text-slate-600">{item.label}</div>
      <div className="min-w-0 text-right text-sm text-slate-800">
        {item.badge || isStatusValue(item.value) ? (
          <span className={`inline-flex rounded-full border px-2 py-1 text-xs font-medium ${statusClass(item.value)}`}>
            {item.value}
          </span>
        ) : (
          <span className="break-words">{item.value}</span>
        )}
      </div>
    </div>
  );
}

export function PipelineOverviewPanel({
  workspaceOverview,
  pipelineWorkspace,
  gitBranches,
  gitBranchesLoading,
  embedded = false,
}: PipelineOverviewPanelProps) {
  const overviewItems = buildPipelineOverviewItems(workspaceOverview, pipelineWorkspace);

  const body = (
    <div className="grid min-h-0 flex-1 grid-cols-[300px_minmax(0,1fr)] gap-4 p-4">
        <section className="flex h-full min-h-0 flex-col rounded-lg border border-slate-200 bg-white p-4">
          <h3 className="border-b border-slate-100 pb-3 text-sm font-semibold text-slate-900">
            Current pipeline overview
          </h3>
          <div className="mt-1 min-h-0 flex-1 overflow-y-auto pr-1">
            {overviewItems.length > 0 ? (
              overviewItems.map((item) => <OverviewFieldRow key={item.id} item={item} />)
            ) : (
              <div className="rounded-[14px] border border-dashed border-slate-200 px-3 py-2 text-sm text-slate-500">
                No pipeline overview data yet.
              </div>
            )}
          </div>
        </section>
        <GitBranchesPanel data={gitBranches} loading={gitBranchesLoading} />
      </div>
  );

  if (embedded) {
    return <section className="flex h-full min-h-0 flex-col bg-white">{body}</section>;
  }

  return (
    <section className="flex min-h-[360px] flex-col rounded-xl border border-slate-200 bg-white shadow-sm">
      <div className="border-b border-slate-100 px-5 py-4">
        <h2 className="text-base font-semibold text-slate-950">Pipeline overview</h2>
      </div>
      {body}
    </section>
  );
}
