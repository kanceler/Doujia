import type {
  DevflowNodeDetailView,
  DevflowGitBranchesView,
  DevflowPluginValidationResultView,
  DevflowPipelineWorkspaceView,
  DevflowSessionArtifactView,
  DevflowSnapshotView,
  DevflowWorkspaceOverviewView,
} from '@shared/devflow-api';
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs';
import { Button } from '@/components/ui/button';
import { ScrollArea } from '@/components/ui/scroll-area';
import { NodeDetailPanel } from './NodeDetailPanel';
import { X } from 'lucide-react';
import { PipelineView } from './PipelineView';
import { PipelineOverviewPanel } from './PipelineOverviewPanel';
import { PreviewAcceptancePanel } from './PreviewAcceptancePanel';
import { FinalResultPanel } from './FinalResultPanel';

export type WorkspaceTab =
  | {
      id: string;
      type: 'pipeline-home';
      title: string;
      workspaceSummary: string[];
      workspaceOverview?: DevflowWorkspaceOverviewView | null;
      pipelineWorkspace?: DevflowPipelineWorkspaceView | null;
      gitBranches?: DevflowGitBranchesView | null;
      gitBranchesLoading?: boolean;
    }
  | {
      id: string;
      type: 'node-detail';
      title: string;
      node: DevflowNodeDetailView;
    }
  | {
      id: string;
      type: 'node-snapshots';
      title: string;
      taskId: string;
      snapshots: DevflowSnapshotView[];
    }
  | {
      id: string;
      type: 'pipeline-graph';
      title: string;
      workspace: DevflowPipelineWorkspaceView;
    }
  | {
      id: string;
      type: 'preview-acceptance';
      title: string;
      runId: string;
    }
  | {
      id: string;
      type: 'final-result';
      title: string;
      runId: string;
      bagId?: string;
      containerId?: string;
      previewUrl?: string;
    }
  | {
      id: string;
      type: 'requirement-doc' | 'pipeline-draft' | 'hot-preview' | 'delivery-preview';
      title: string;
      artifact?: DevflowSessionArtifactView;
      content?: string;
    }
  | {
      id: string;
      type: 'plugin-validation';
      title: string;
      validation: DevflowPluginValidationResultView;
    };

interface WorkspaceTabsProps {
  tabs: WorkspaceTab[];
  activeTabId: string | null;
  onTabChange: (tabId: string) => void;
  onCloseTab: (tabId: string) => void;
  onOpenPipeline: (instanceId: string, title: string) => void;
  onOpenTaskSnapshots: (taskId: string, title: string) => void;
}

export const WorkspaceTabs: React.FC<WorkspaceTabsProps> = ({
  tabs,
  activeTabId,
  onTabChange,
  onCloseTab,
  onOpenPipeline,
  onOpenTaskSnapshots,
}) => {
  const visibleTabs = tabs.length > 0 ? tabs : [buildFallbackHomeTab()];
  const homeTab = visibleTabs.find((tab) => tab.type === 'pipeline-home') ?? buildFallbackHomeTab();
  const activeTab = activeTabId && visibleTabs.some((tab) => tab.id === activeTabId) ? activeTabId : homeTab.id;

  return (
    <Tabs value={activeTab} onValueChange={onTabChange} className="flex h-full min-h-0 flex-col gap-0">
      <div className="border-b border-slate-200/70 bg-white/55 px-3 py-2 backdrop-blur">
        <ScrollArea className="w-full whitespace-nowrap">
          <TabsList className="inline-flex h-auto gap-1 rounded-none bg-transparent p-0">
            {visibleTabs.map((tab) => (
              <div key={tab.id} className="inline-flex items-center gap-1">
                <TabsTrigger value={tab.id} className="h-8 rounded-full border border-slate-200/80 bg-white/88 px-3 text-xs shadow-[0_6px_14px_rgba(15,23,42,0.04)]">
                  {tab.title}
                </TabsTrigger>
                {tab.type !== 'pipeline-home' && tab.type !== 'preview-acceptance' && tab.type !== 'final-result' && (
                  <Button variant="ghost" size="icon" className="size-7 rounded-full" onClick={() => onCloseTab(tab.id)} aria-label={`Close ${tab.title}`}>
                    <X className="size-3.5" />
                  </Button>
                )}
              </div>
            ))}
          </TabsList>
        </ScrollArea>
      </div>

      {visibleTabs.map((tab) => (
        <TabsContent key={tab.id} value={tab.id} className="min-h-0 flex-1">
          <WorkspaceTabBody
            tab={tab}
            onOpenPipeline={onOpenPipeline}
            onOpenTaskSnapshots={onOpenTaskSnapshots}
          />
        </TabsContent>
      ))}
    </Tabs>
  );
};

const WorkspaceTabBody: React.FC<{
  tab: WorkspaceTab;
  onOpenPipeline: (instanceId: string, title: string) => void;
  onOpenTaskSnapshots: (taskId: string, title: string) => void;
}> = ({ tab, onOpenPipeline, onOpenTaskSnapshots }) => {
  switch (tab.type) {
    case 'pipeline-home':
      return (
        <PipelineOverviewPanel
          workspaceOverview={tab.workspaceOverview}
          pipelineWorkspace={tab.pipelineWorkspace}
          gitBranches={tab.gitBranches}
          gitBranchesLoading={tab.gitBranchesLoading}
          embedded
        />
      );
    case 'node-detail':
      return <NodeDetailPanel node={tab.node} />;
    case 'node-snapshots':
      return <NodeSnapshotsTabPanel taskId={tab.taskId} snapshots={tab.snapshots} />;
    case 'pipeline-graph':
      return (
        <PipelineView
          key={tab.id}
          workspace={tab.workspace}
          title={tab.title}
          onOpenPipeline={onOpenPipeline}
          onOpenTaskSnapshots={onOpenTaskSnapshots}
        />
      );
    case 'preview-acceptance':
      return <PreviewAcceptancePanel runId={tab.runId} />;
    case 'final-result':
      return (
        <FinalResultPanel
          runId={tab.runId}
          bagId={tab.bagId}
          containerId={tab.containerId}
          previewUrl={tab.previewUrl}
        />
      );
    case 'requirement-doc':
    case 'pipeline-draft':
      return (
        <ScrollArea className="h-full">
          <pre className="whitespace-pre-wrap p-4 text-sm leading-7 text-foreground">
            {tab.artifact?.content || tab.content || 'No content'}
          </pre>
        </ScrollArea>
      );
    case 'plugin-validation':
      return (
        <ScrollArea className="h-full">
          <div className="space-y-4 p-4 text-sm">
            <section>
              <div className="font-medium text-foreground">Status</div>
              <div className="mt-1 text-muted-foreground">{tab.validation.status}</div>
            </section>
            <section>
              <div className="font-medium text-foreground">File</div>
              <div className="mt-1 break-all font-mono text-xs text-muted-foreground">{tab.validation.file_name}</div>
            </section>
            <section>
              <div className="font-medium text-foreground">Activated resources</div>
              <pre className="mt-2 rounded-md border border-border bg-muted/40 p-3 text-xs">
                {JSON.stringify(
                  {
                    activated_handlers: tab.validation.activated_handlers ?? [],
                    activated_ops: tab.validation.activated_ops ?? [],
                    activated_roles: tab.validation.activated_roles ?? [],
                    activated_pipelines: tab.validation.activated_pipelines ?? [],
                  },
                  null,
                  2,
                )}
              </pre>
            </section>
            {(tab.validation.errors?.length ?? 0) > 0 && (
              <section>
                <div className="font-medium text-foreground">Errors</div>
                <ul className="mt-2 space-y-2">
                  {tab.validation.errors?.map((error) => (
                    <li key={error} className="rounded-md border border-destructive/30 bg-destructive/5 px-3 py-2 text-xs text-destructive">
                      {error}
                    </li>
                  ))}
                </ul>
              </section>
            )}
          </div>
        </ScrollArea>
      );
    case 'hot-preview':
    case 'delivery-preview':
      return <div className="flex h-full items-center justify-center p-6 text-sm text-muted-foreground">Preview container reserved for future runtime integration.</div>;
    default:
      return null;
  }
};

function NodeSnapshotsTabPanel({ taskId, snapshots }: { taskId: string; snapshots: DevflowSnapshotView[] }) {
  return (
    <ScrollArea className="h-full">
      <div className="space-y-3 p-4">
        <div className="text-sm font-semibold text-slate-900">Snapshots for {taskId}</div>
        {snapshots.length === 0 ? (
          <div className="rounded-[14px] border border-dashed border-slate-200 px-3 py-2 text-sm text-slate-500">No snapshots yet.</div>
        ) : (
          snapshots.map((snapshot) => (
            <div key={snapshot.snapshot_id} className="rounded-[16px] border border-slate-200/80 bg-white/72 p-3 text-sm shadow-[0_4px_14px_rgba(15,23,42,0.03)]">
              <div className="flex items-center justify-between gap-2">
                <div className="font-mono text-xs text-muted-foreground">{snapshot.snapshot_id}</div>
                <span className="rounded-full border border-slate-200 px-2 py-0.5 text-[10px] text-muted-foreground">{snapshot.result}</span>
              </div>
              <div className="mt-2 text-xs text-muted-foreground">{snapshot.transition_id || snapshot.op || 'snapshot'}</div>
              <div className="mt-2 break-all font-mono text-[11px] text-muted-foreground">{snapshot.output_bag_ids?.join(', ') || 'No output bags'}</div>
            </div>
          ))
        )}
      </div>
    </ScrollArea>
  );
}

function buildFallbackHomeTab(): WorkspaceTab {
  return {
    id: 'pipeline-home',
    type: 'pipeline-home',
    title: 'Pipeline overview',
    workspaceSummary: ['No instantiated pipeline workspace yet.'],
  };
}

export function upsertWorkspaceTab(tabs: WorkspaceTab[], tab: WorkspaceTab): WorkspaceTab[] {
  const existingTab = tabs.find((item) => item.id === tab.id);
  if (existingTab) {
    return tabs.map((item) => (item.id === tab.id ? tab : item));
  }
  const homeTab = tabs.find((item) => item.type === 'pipeline-home');
  const detailTabs = tabs.filter((item) => item.type !== 'pipeline-home');
  if (tab.type === 'pipeline-home') {
    return [tab, ...detailTabs];
  }
  return homeTab ? [homeTab, ...detailTabs, tab] : [...detailTabs, tab];
}

export const WORKSPACE_TAB_ID_EXAMPLES = {
  pipelineHome: 'pipeline-home',
  nodeDetail: 'node-detail:{task_id}',
  nodeSnapshots: 'node-snapshots:{task_id}',
  pipelineGraph: 'pipeline-graph:{instance_id}',
  requirementDoc: 'requirement-doc:{iteration_no}',
  pipelineDraft: 'pipeline-draft:{iteration_no}',
  pluginValidation: 'plugin-validation:{job_id}',
  previewAcceptance: 'preview-acceptance',
  finalResult: 'final-result',
  hotPreview: 'hot-preview:{id}',
  deliveryPreview: 'delivery-preview:{id}',
};

export function setActiveTabId(tabId: string | null): string | null {
  return tabId;
}
