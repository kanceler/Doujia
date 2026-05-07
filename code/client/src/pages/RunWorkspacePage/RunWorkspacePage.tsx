import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import type { MessageItem, MessageRole, RunStatus } from '@shared/api.interface';
import type {
  DevflowAcceptanceCheckpointView,
  DevflowGitBranchesView,
  DevflowImprovementItemView,
  DevflowPipelineWorkspaceView,
  DevflowRunView,
  DevflowSessionArtifactView,
  DevflowSessionMessageView,
  DevflowTaskView,
  DevflowWorkspaceOverviewView,
} from '@shared/devflow-api';
import { useParams } from 'react-router-dom';
import {
  approveDevflowAcceptanceCheckpoint,
  confirmDevflowRequirements,
  confirmDevflowRequirementSummary,
  continueDevflowAcceptanceCheckpoint,
  createDevflowImprovementItem,
  getDevflowAcceptanceCheckpoint,
  getDevflowGitBranches,
  getDevflowImprovementItems,
  getDevflowNodeDetail,
  getDevflowPipelineGraph,
  getDevflowPipelineWorkspace,
  getDevflowRun,
  getDevflowSessionMessages,
  getDevflowTasks,
  getDevflowWorkspaceOverview,
  patchDevflowImprovementItem,
  postDevflowSessionMessage,
  postDevflowSessionMessageStream,
  removeDevflowImprovementItem,
} from '@/api/devflow-client';
import { Empty, EmptyDescription, EmptyHeader, EmptyMedia, EmptyTitle } from '@/components/ui/empty';
import { Spinner } from '@/components/ui/spinner';
import { AlertTriangle, Sparkles, X } from 'lucide-react';
import { AcceptanceContinueDialog } from './AcceptanceContinueDialog';
import { ImprovementItemBar } from './ImprovementItemBar';
import { MessageFlow } from './MessageFlow';
import { PipelineView } from './PipelineView';
import { SessionComposer } from './SessionComposer';
import { WorkspaceTabs, type WorkspaceTab, upsertWorkspaceTab } from './WorkspaceTabs';

const RunWorkspacePage: React.FC = () => {
  const { runId } = useParams<{ runId: string }>();
  const [run, setRun] = useState<DevflowRunView | null>(null);
  const [tasks, setTasks] = useState<DevflowTaskView[]>([]);
  const [sessionMessages, setSessionMessages] = useState<DevflowSessionMessageView[]>([]);
  const [improvementItems, setImprovementItems] = useState<DevflowImprovementItemView[]>([]);
  const [workspaceOverview, setWorkspaceOverview] = useState<DevflowWorkspaceOverviewView | null>(null);
  const [pipelineWorkspace, setPipelineWorkspace] = useState<DevflowPipelineWorkspaceView | null>(null);
  const [gitBranches, setGitBranches] = useState<DevflowGitBranchesView | null>(null);
  const [acceptanceCheckpoint, setAcceptanceCheckpoint] = useState<DevflowAcceptanceCheckpointView | null>(null);
  const [tabs, setTabs] = useState<WorkspaceTab[]>([]);
  const [activeTabId, setActiveTabId] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [gitBranchesLoading, setGitBranchesLoading] = useState(false);
  const [continueDialogOpen, setContinueDialogOpen] = useState(false);
  const [acceptanceLoading, setAcceptanceLoading] = useState(false);
  const [requirementMutating, setRequirementMutating] = useState(false);
  const [sessionSending, setSessionSending] = useState(false);
  const [streamingAssistantMessage, setStreamingAssistantMessage] = useState<MessageItem | null>(null);
  const messagesEndRef = useRef<HTMLDivElement>(null);

  const hasProjectLLM = Boolean(
    run?.project_id &&
      (
        run.config?.llm?.model ||
        run.config?.llm?.provider ||
        run.config?.llm?.api_key ||
        run.config?.llm?.base_url ||
        run.config?.llm?.api_style
      ),
  );

  const fetchData = useCallback(async () => {
    if (!runId) return;
    setGitBranchesLoading(true);
    try {
      const [
        runData,
        taskData,
        messageData,
        improvementData,
        acceptanceData,
        overviewData,
        pipelineWorkspaceData,
        gitBranchesData,
      ] = await Promise.all([
        getDevflowRun(runId),
        getDevflowTasks(runId),
        getDevflowSessionMessages(runId).catch(() => ({ items: [] })),
        getDevflowImprovementItems(runId).catch(() => ({ items: [] })),
        getDevflowAcceptanceCheckpoint(runId).catch(() => null),
        getDevflowWorkspaceOverview(runId).catch(() => null),
        getDevflowPipelineWorkspace(runId).catch(() => null),
        getDevflowGitBranches(runId).catch(() => null),
      ]);

      setRun(runData);
      setTasks(taskData.items);
      setSessionMessages(messageData.items);
      setImprovementItems(improvementData.items);
      setAcceptanceCheckpoint(acceptanceData);
      setWorkspaceOverview(overviewData);
      setPipelineWorkspace(pipelineWorkspaceData);
      setGitBranches(gitBranchesData);
      setError(null);
      setLoading(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to load workspace data');
      setLoading(false);
    } finally {
      setGitBranchesLoading(false);
    }
  }, [runId]);

  useEffect(() => {
    void fetchData();
  }, [fetchData]);

  useEffect(() => {
    if (!run || !runId) return;
    const shouldPoll =
      run.status === 'running' ||
      run.status === 'awaiting_acceptance' ||
      tasks.some((task) => task.status === 'waiting_external' || task.status === 'running' || task.status === 'dispatched');
    if (!shouldPoll) return;
    const timer: ReturnType<typeof setInterval> = setInterval(() => {
      void fetchData();
    }, 2000);
    return () => clearInterval(timer);
  }, [run?.status, runId, tasks, fetchData]);

  const sessionArtifactsByKind = useMemo(() => {
    const byKind = new Map<string, DevflowSessionArtifactView>();
    for (const artifact of workspaceOverview?.session_artifacts ?? []) {
      byKind.set(`${artifact.kind}:${artifact.iteration_no}`, artifact);
    }
    return byKind;
  }, [workspaceOverview?.session_artifacts]);

  const openTab = useCallback((tab: WorkspaceTab) => {
    setTabs((current) => upsertWorkspaceTab(current, tab));
    setActiveTabId(tab.id);
  }, []);

  const handleOpenNodeSnapshots = useCallback(async (taskId: string, title: string) => {
    if (!runId) return;
    const graph = await getDevflowPipelineGraph(runId, 'main');
    const snapshots = graph.snapshots.filter((snapshot) => snapshot.task_id === taskId);
    openTab({
      id: `node-snapshots:${taskId}`,
      type: 'node-snapshots',
      title,
      taskId,
      snapshots,
    });
  }, [openTab, runId]);

  const handleOpenPipeline = useCallback(async (instanceId: string, title: string) => {
    if (!runId) return;
    const focused = await getDevflowPipelineWorkspace(runId, instanceId);
    openTab({
      id: `pipeline-graph:${instanceId}`,
      type: 'pipeline-graph',
      title,
      workspace: focused,
    });
  }, [openTab, runId]);

  const handleNodeSelect = async (taskId: string) => {
    if (!runId) return;
    try {
      const detail = await getDevflowNodeDetail(runId, taskId);
      openTab({
        id: `node-detail:${detail.task.task_id}`,
        type: 'node-detail',
        title: detail.task.stage_id || detail.task.task_id,
        node: detail,
      });
    } catch {
      // keep current tabs intact on transient failure
    }
  };

  const handleCloseTab = (tabId: string) => {
    setTabs((current) => {
      const next = current.filter((item) => item.id !== tabId);
      if (activeTabId === tabId) {
        const replacement = next[next.length - 1];
        setActiveTabId(replacement?.id ?? null);
      }
      return next;
    });
  };

  const handleActionComplete = () => {
    void fetchData();
  };

  const finalResultIdentity = useMemo(
    () => resolveFinalResultIdentity(sessionMessages, tasks, gitBranches),
    [sessionMessages, tasks, gitBranches],
  );

  const handleOpenGeneratedProject = () => {
    setActiveTabId('final-result');
  };

  const handleConfirmRequirementSummary = async (messageId: string) => {
    if (!runId) return;
    setRequirementMutating(true);
    try {
      await confirmDevflowRequirementSummary(runId, messageId, 'accept');
      await fetchData();
    } finally {
      setRequirementMutating(false);
    }
  };

  const handleSendSessionMessage = async (content: string) => {
    if (!runId) return;
    setSessionSending(true);
    setStreamingAssistantMessage({
      id: `streaming-doujia-${Date.now()}`,
      runId,
      role: 'assistant',
      type: 'text',
      content: '',
      metadata: null,
      createdAt: new Date().toISOString(),
    });
    try {
      await postDevflowSessionMessageStream(runId, {
        content,
        message_type: 'chat',
      }, {
        onDelta: (delta) => {
          setStreamingAssistantMessage((current) => {
            if (!current) return current;
            return {
              ...current,
              content: current.content + delta,
            };
          });
        },
      });
      await fetchData();
    } finally {
      setStreamingAssistantMessage(null);
      setSessionSending(false);
    }
  };

  const handleApproveAcceptance = async () => {
    if (!runId) return;
    setAcceptanceLoading(true);
    try {
      await approveDevflowAcceptanceCheckpoint(runId, { comment: 'Accepted from workspace' });
      await fetchData();
    } finally {
      setAcceptanceLoading(false);
    }
  };

  const handleConfirmRequirements = async () => {
    if (!runId) return;
    setRequirementMutating(true);
    try {
      await confirmDevflowRequirements(runId);
      await fetchData();
    } finally {
      setRequirementMutating(false);
    }
  };

  const handleRemoveRequirementItem = async (itemId: string) => {
    if (!runId) return;
    setRequirementMutating(true);
    try {
      await removeDevflowImprovementItem(runId, itemId);
      await fetchData();
    } finally {
      setRequirementMutating(false);
    }
  };

  const handleContinueAcceptance = async (payload: { selected_item_ids: string[]; freeform_text: string }) => {
    if (!runId) return;
    setAcceptanceLoading(true);
    try {
      for (const item of improvementItems) {
        const selected = payload.selected_item_ids.includes(item.item_id);
        const nextStatus = selected ? 'selected' : item.status === 'selected' ? 'open' : item.status;
        if (nextStatus !== item.status) {
          await patchDevflowImprovementItem(runId, item.item_id, { status: nextStatus });
        }
      }
      if (payload.freeform_text.trim()) {
        await postDevflowSessionMessage(runId, {
          content: payload.freeform_text.trim(),
          message_type: 'acceptance_continue_note',
        });
        await createDevflowImprovementItem(runId, {
          title: summarizeTitle(payload.freeform_text),
          detail: payload.freeform_text.trim(),
          source: 'user',
        });
      }
      await continueDevflowAcceptanceCheckpoint(runId, payload);
      await fetchData();
    } finally {
      setAcceptanceLoading(false);
    }
  };

  const handleOpenLatestArtifacts = () => {
    if (!workspaceOverview) return;
    const requirement = sessionArtifactsByKind.get(`requirement_doc:${workspaceOverview.current_iteration_no}`);
    if (requirement) {
      openTab({
        id: `requirement-doc:${requirement.iteration_no}`,
        type: 'requirement-doc',
        title: `Requirement ${requirement.iteration_no}`,
        artifact: requirement,
      });
    }
    const pipelineDraft = sessionArtifactsByKind.get(`pipeline_draft:${workspaceOverview.current_iteration_no}`);
    if (pipelineDraft) {
      openTab({
        id: `pipeline-draft:${pipelineDraft.iteration_no}`,
        type: 'pipeline-draft',
        title: `Pipeline Draft ${pipelineDraft.iteration_no}`,
        artifact: pipelineDraft,
      });
    }
  };

  useEffect(() => {
    if (!workspaceOverview) return;
    handleOpenLatestArtifacts();
    // open once when overview changes
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [workspaceOverview?.current_iteration_no]);

  const confirmedRequirementItems = useMemo(
    () =>
      improvementItems.filter(
        (item) => item.source === 'requirement_pool_confirmed' && item.status === 'confirmed',
      ),
    [improvementItems],
  );

  const waitingForRequirementConfirmation =
    run?.status === 'running' &&
    tasks.some(
      (task) =>
        task.task_id === 'ceo_write_requirement' &&
        task.status === 'waiting_external',
    );

  const messages = useMemo(
    () =>
      toDialogMessages(
        run,
        tasks,
        sessionMessages,
        improvementItems,
        acceptanceCheckpoint,
        handleConfirmRequirementSummary,
      ),
    [run, tasks, sessionMessages, improvementItems, acceptanceCheckpoint],
  );
  const visibleMessages = streamingAssistantMessage ? [...messages, streamingAssistantMessage] : messages;

  useEffect(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, [streamingAssistantMessage?.content, visibleMessages.length]);

  if (loading) {
    return (
      <div className="flex h-full items-center justify-center bg-[#f7f8fb]">
        <Spinner className="size-8" />
      </div>
    );
  }

  if (error || !run) {
    return (
      <div className="flex h-full items-center justify-center bg-[#f7f8fb] p-8">
        <Empty>
          <EmptyHeader>
            <EmptyMedia variant="icon">
              <AlertTriangle className="size-6 text-destructive" />
            </EmptyMedia>
            <EmptyTitle>Workspace load failed</EmptyTitle>
            <EmptyDescription>{error || 'Unable to load run data'}</EmptyDescription>
          </EmptyHeader>
        </Empty>
      </div>
    );
  }

  const workspaceTabs: WorkspaceTab[] = [
    {
      id: 'pipeline-home',
      type: 'pipeline-home',
      title: 'Pipeline overview',
      workspaceSummary: [],
      workspaceOverview,
      pipelineWorkspace,
      gitBranches,
      gitBranchesLoading,
    },
    ...(runId
      ? [
          {
            id: 'final-result',
            type: 'final-result' as const,
            title: '最终结果',
            runId,
            bagId: finalResultIdentity.bagId,
            containerId: finalResultIdentity.containerId,
            previewUrl: finalResultIdentity.previewUrl,
          },
          {
            id: 'preview-acceptance',
            type: 'preview-acceptance' as const,
            title: '预览验收',
            runId,
          },
        ]
      : []),
    ...tabs.filter((tab) => tab.type !== 'pipeline-home' && tab.type !== 'preview-acceptance' && tab.type !== 'final-result'),
  ];
  const activeWorkspaceTab =
    activeTabId && workspaceTabs.some((tab) => tab.id === activeTabId)
      ? activeTabId
      : 'pipeline-home';

  return (
    <div className="relative h-full min-h-0 overflow-auto bg-slate-50">
      <div className="grid h-full min-h-[720px] min-w-[1180px] grid-cols-[460px_minmax(720px,1fr)] gap-4 p-4 xl:p-6">
        <section className="flex min-h-0 flex-col overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm">
          <div className="shrink-0 border-b border-slate-100 bg-white">
            <div className="flex flex-col gap-4 px-4 py-4">
            <div className="flex flex-wrap items-center justify-between gap-4">
              <div className="flex min-w-0 items-center gap-3">
                <div className="flex size-11 shrink-0 items-center justify-center rounded-[18px] bg-[radial-gradient(circle_at_top,#ffffff_0%,rgba(224,231,255,0.95)_32%,rgba(191,219,254,0.92)_68%,rgba(96,165,250,0.18)_100%)] text-primary shadow-[0_14px_34px_rgba(59,130,246,0.16)]">
                  <Sparkles className="size-5" />
                </div>
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <h1 className="truncate text-[22px] font-semibold tracking-[-0.03em] text-slate-950">
                      Doujia 对话
                    </h1>
                    <StatusBadge status={mapRunStatus(run.status)} />
                  </div>
                  <div className="mt-1 flex flex-wrap items-center gap-2 text-xs text-slate-500">
                    <span className="rounded-full border border-slate-200/80 bg-white/80 px-2.5 py-1 font-medium text-slate-600">
                      {run.run_id}
                    </span>
                    <span className="rounded-full bg-slate-100/80 px-2.5 py-1 font-mono text-slate-500">
                      iteration {run.current_iteration_no}
                    </span>
                  </div>
                </div>
              </div>

              <div className="hidden items-center gap-2 rounded-full border border-emerald-200/80 bg-emerald-50/90 px-3 py-1.5 text-xs font-medium text-emerald-700 md:flex">
                <span className="size-2 rounded-full bg-emerald-500 animate-pulse" />
                真流式对话
              </div>
            </div>

            {run.status === 'awaiting_acceptance' && (
              <div className="rounded-[24px] border border-slate-200/80 bg-white/75 px-4 py-4 shadow-[0_12px_30px_rgba(15,23,42,0.05)]">
                <div className="flex flex-wrap items-center justify-between gap-3">
                  <div>
                    <div className="text-sm font-semibold text-slate-950">当前结果等待你验收</div>
                    <div className="mt-1 text-xs text-slate-500">
                      你可以直接通过当前交付结果，或者继续补充改进项进入下一轮。
                    </div>
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    <button
                      type="button"
                      className="rounded-full border border-slate-200 bg-white px-4 py-2 text-sm text-slate-700 shadow-[0_6px_18px_rgba(15,23,42,0.04)] transition hover:border-primary/35 hover:text-primary disabled:cursor-not-allowed disabled:opacity-60"
                      onClick={() => void handleApproveAcceptance()}
                      disabled={acceptanceLoading}
                    >
                      {acceptanceLoading ? '处理中...' : '通过验收'}
                    </button>
                    <button
                      type="button"
                      className="rounded-full border border-slate-200 bg-slate-50 px-4 py-2 text-sm text-slate-700 transition hover:border-primary/35 hover:bg-white disabled:cursor-not-allowed disabled:opacity-60"
                      onClick={() => setContinueDialogOpen(true)}
                      disabled={acceptanceLoading}
                    >
                      继续补充
                    </button>
                  </div>
                </div>
              </div>
            )}

            {waitingForRequirementConfirmation && (
              <div className="rounded-[24px] border border-amber-200/80 bg-amber-50/88 px-4 py-4 shadow-[0_12px_30px_rgba(245,158,11,0.08)]">
                <div className="flex flex-col gap-3">
                  <div className="flex flex-wrap items-start justify-between gap-3">
                    <div className="space-y-1">
                      <div className="text-sm font-semibold text-slate-950">Doujia 正在收敛需求</div>
                      <div className="text-xs leading-5 text-amber-900/75">
                        确认后，Doujia 会结合当前需求池和聊天记录生成需求文档，并继续启动后续流程。
                      </div>
                    </div>
                    <button
                      type="button"
                      className="rounded-full border border-amber-200 bg-white px-4 py-2 text-sm text-amber-900 shadow-[0_8px_20px_rgba(245,158,11,0.08)] transition hover:border-amber-300 hover:bg-amber-50 disabled:cursor-not-allowed disabled:opacity-60"
                      onClick={() => void handleConfirmRequirements()}
                      disabled={requirementMutating || confirmedRequirementItems.length === 0}
                    >
                      {requirementMutating ? '启动中...' : '确认需求并开始'}
                    </button>
                  </div>
                  <div className="rounded-[20px] border border-white/70 bg-white/82 px-4 py-4">
                    <div className="mb-3 text-xs font-semibold tracking-[0.02em] text-slate-600">
                      当前需求池 {confirmedRequirementItems.length > 0 ? `(${confirmedRequirementItems.length})` : ''}
                    </div>
                    {confirmedRequirementItems.length > 0 ? (
                      <div className="flex flex-wrap gap-2">
                        {confirmedRequirementItems.map((item) => (
                          <div
                            key={item.item_id}
                            className="inline-flex max-w-full items-center gap-2 rounded-full border border-slate-200/80 bg-slate-50 px-3 py-1.5 text-sm text-slate-700"
                          >
                            <span className="truncate">{item.detail || item.title}</span>
                            <button
                              type="button"
                              aria-label="Remove requirement item"
                              className="text-slate-400 transition hover:text-slate-700 disabled:opacity-50"
                              disabled={requirementMutating}
                              onClick={() => void handleRemoveRequirementItem(item.item_id)}
                            >
                              <X className="size-3.5" />
                            </button>
                          </div>
                        ))}
                      </div>
                    ) : (
                      <div className="text-sm text-slate-500">
                        先在下方和 Doujia 对话，再逐条确认需求摘要，需求池就会逐步成形。
                      </div>
                    )}
                  </div>
                </div>
              </div>
            )}
          </div>
        </div>

        <ImprovementItemBar
          items={improvementItems}
          awaitingAcceptance={run.status === 'awaiting_acceptance'}
          onOpenContinueDialog={() => setContinueDialogOpen(true)}
        />

        <div className="min-h-0 flex-1 overflow-hidden">
          <MessageFlow
            messages={visibleMessages}
            onActionComplete={handleActionComplete}
            onProjectGeneratedClick={handleOpenGeneratedProject}
            messagesEndRef={messagesEndRef}
          />
        </div>
        <div className="shrink-0 border-t border-slate-100 bg-white">
          <SessionComposer
            disabled={!runId || !hasProjectLLM}
            disabledReason={!hasProjectLLM ? '请先为当前项目配置可用的 LLM 模型。' : null}
            loading={sessionSending}
            onSend={handleSendSessionMessage}
          />
        </div>
        </section>

        <section className="grid min-h-0 grid-rows-[minmax(220px,0.55fr)_minmax(420px,1.45fr)] gap-4">
          <PipelineView
            workspace={pipelineWorkspace}
            onOpenPipeline={handleOpenPipeline}
            onOpenTaskSnapshots={handleOpenNodeSnapshots}
          />
          <section className="min-h-0 overflow-hidden rounded-xl border border-slate-200 bg-white shadow-sm">
            <WorkspaceTabs
              tabs={workspaceTabs}
              activeTabId={activeWorkspaceTab}
              onTabChange={setActiveTabId}
              onCloseTab={handleCloseTab}
              onOpenPipeline={handleOpenPipeline}
              onOpenTaskSnapshots={handleOpenNodeSnapshots}
            />
          </section>
        </section>
      </div>

      <AcceptanceContinueDialog
        open={continueDialogOpen}
        items={improvementItems}
        currentIterationNo={run.current_iteration_no}
        loading={acceptanceLoading}
        onOpenChange={setContinueDialogOpen}
        onSubmit={handleContinueAcceptance}
      />
    </div>
  );
};

const statusColorMap: Record<RunStatus, string> = {
  pending: 'border-amber-200/80 bg-amber-50 text-amber-700',
  running: 'border-blue-200/80 bg-blue-50 text-blue-700',
  success: 'border-green-200/80 bg-green-50 text-green-700',
  failed: 'border-red-200/80 bg-red-50 text-red-700',
  rejected: 'border-slate-200/80 bg-slate-100 text-slate-700',
  recovered: 'border-purple-200/80 bg-purple-50 text-purple-700',
};

const statusLabelMap: Record<RunStatus, string> = {
  pending: '等待中',
  running: '进行中',
  success: '已完成',
  failed: '失败',
  rejected: '待验收',
  recovered: '已恢复',
};

const StatusBadge: React.FC<{ status: RunStatus }> = ({ status }) => {
  const colors = statusColorMap[status];
  const label = statusLabelMap[status];
  return (
    <span className={`inline-flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium shadow-[0_6px_18px_rgba(15,23,42,0.04)] ${colors}`}>
      {status === 'running' && <span className="size-1.5 animate-pulse rounded-full bg-current" />}
      {label}
    </span>
  );
};

function mapRunStatus(status: DevflowRunView['status']): RunStatus {
  switch (status) {
    case 'created':
      return 'pending';
    case 'running':
      return 'running';
    case 'awaiting_acceptance':
      return 'rejected';
    case 'completed':
      return 'success';
    case 'failed':
      return 'failed';
    default:
      return 'pending';
  }
}

function toDialogMessages(
  run: DevflowRunView | null,
  tasks: DevflowTaskView[],
  sessionMessages: DevflowSessionMessageView[],
  improvementItems: DevflowImprovementItemView[],
  acceptanceCheckpoint: DevflowAcceptanceCheckpointView | null,
  onConfirmRequirementSummary: (messageId: string) => Promise<void> | void,
): MessageItem[] {
  const taskMessages: MessageItem[] = tasks
    .filter((task) => task.status === 'waiting_external' && task.task_id !== 'ceo_write_requirement')
    .map((task) => ({
      id: task.task_id,
      runId: task.run_id,
      role: 'assistant' as MessageRole,
      type: 'approval' as const,
      content: `${task.agent_role}/${task.agent_id} is waiting for human confirmation: ${task.op}`,
      metadata: { approvalStatus: 'pending' },
      createdAt: task.updated_at || task.created_at,
    }));

  const chatMessages: MessageItem[] = sessionMessages.map((message) => {
    const isRequirementSummary = message.message_type === 'requirement_summary';
    const isProjectGenerated = message.message_type === 'project_generated';
    const summaryKey =
      typeof message.metadata?.summary_key === 'string'
        ? message.metadata.summary_key.trim()
        : message.content.trim();
    const confirmed = improvementItems.some(
      (item) =>
        item.source === 'requirement_pool_confirmed' &&
        item.status === 'confirmed' &&
        (item.detail || item.title).trim() === summaryKey,
    );

    return {
      id: message.message_id,
      runId: message.run_id,
      role: (message.role === 'user' ? 'user' : 'assistant') as MessageRole,
      type: (
        isRequirementSummary
          ? 'requirement-summary'
          : isProjectGenerated
            ? 'project-generated'
            : 'text'
      ) as MessageItem['type'],
      content: message.content,
      metadata: isRequirementSummary
        ? {
            requirementSummary: {
              confirmActionPath:
                typeof message.metadata?.confirm_action_path === 'string'
                  ? message.metadata.confirm_action_path
                  : undefined,
              confirmActionLabel:
                typeof message.metadata?.confirm_action_label === 'string'
                  ? message.metadata.confirm_action_label
                  : undefined,
              reviseActionLabel:
                typeof message.metadata?.revise_action_label === 'string'
                  ? message.metadata.revise_action_label
                  : undefined,
              messageId: message.message_id,
              confirmed,
              onConfirm: confirmed ? undefined : () => onConfirmRequirementSummary(message.message_id),
            },
          }
        : isProjectGenerated
          ? {
              projectGenerated: {
                title:
                  typeof message.metadata?.title === 'string'
                    ? message.metadata.title
                    : undefined,
                description:
                  typeof message.metadata?.description === 'string'
                    ? message.metadata.description
                    : undefined,
                actionLabel:
                  typeof message.metadata?.action_label === 'string'
                    ? message.metadata.action_label
                    : undefined,
                targetTabId:
                  typeof message.metadata?.target_tab_id === 'string'
                    ? message.metadata.target_tab_id
                    : 'final-result',
                previewUrl:
                  typeof message.metadata?.preview_url === 'string'
                    ? message.metadata.preview_url
                    : undefined,
                artifactId:
                  typeof message.metadata?.artifact_id === 'string'
                    ? message.metadata.artifact_id
                    : undefined,
                bagId:
                  typeof message.metadata?.bag_id === 'string'
                    ? message.metadata.bag_id
                    : typeof message.metadata?.bagId === 'string'
                      ? message.metadata.bagId
                      : undefined,
                containerId:
                  typeof message.metadata?.container_id === 'string'
                    ? message.metadata.container_id
                    : typeof message.metadata?.containerId === 'string'
                      ? message.metadata.containerId
                      : undefined,
              },
            }
        : null,
      createdAt: message.created_at,
    };
  });

  const improvementSummary = improvementItems
    .filter((item) => item.status === 'open')
    .map((item) => `- ${item.title}: ${item.detail}`)
    .join('\n');

  const acceptanceMessage:
    | MessageItem[]
    | [] = acceptanceCheckpoint && acceptanceCheckpoint.status === 'awaiting_acceptance'
    ? [{
        id: `acceptance-${acceptanceCheckpoint.checkpoint_task_id}`,
        runId: acceptanceCheckpoint.run_id,
        role: 'assistant' as const,
        type: 'text' as const,
        content: `The current delivery round is waiting for acceptance.\n\nIteration ${acceptanceCheckpoint.current_iteration_no}\nCheckpoint: ${acceptanceCheckpoint.checkpoint_task_id}${improvementSummary ? `\n\nQueued improvement items:\n${improvementSummary}` : ''}`,
        metadata: null,
        createdAt: new Date().toISOString(),
      }]
    : [];

  const hasPersistedProjectGeneratedMessage = chatMessages.some(
    (message) => message.type === 'project-generated',
  );
  const generatedProjectMessage: MessageItem[] =
    run?.status === 'completed' && !hasPersistedProjectGeneratedMessage
      ? [
          {
            id: 'generated-project-card',
            runId: run.run_id,
            role: 'assistant',
            type: 'project-generated',
            content: '项目已经生成，可以查看生成结果并进行预览验收。',
            metadata: {
              projectGenerated: {
                title: '项目已经生成',
                description: '代码、测试和合并结果已经完成，可以进入预览验收查看生成效果。',
                actionLabel: '点击查看',
                targetTabId: 'final-result',
              },
            },
            createdAt: run.updated_at || new Date().toISOString(),
          },
        ]
      : [];

  return [...chatMessages, ...taskMessages, ...acceptanceMessage, ...generatedProjectMessage];
}

function summarizeTitle(content: string): string {
  const trimmed = content.trim().replace(/\s+/g, ' ');
  if (!trimmed) return 'Additional improvement';
  return trimmed.length > 40 ? `${trimmed.slice(0, 40)}...` : trimmed;
}

function resolveFinalResultIdentity(
  sessionMessages: DevflowSessionMessageView[],
  tasks: DevflowTaskView[],
  gitBranches: DevflowGitBranchesView | null,
): { bagId?: string; containerId?: string; previewUrl?: string } {
  for (const message of [...sessionMessages].reverse()) {
    if (message.message_type !== 'project_generated') continue;
    const metadata = message.metadata;
    const bagId = readStringMetadata(metadata, 'bag_id') ?? readStringMetadata(metadata, 'bagId');
    const containerId =
      readStringMetadata(metadata, 'container_id') ?? readStringMetadata(metadata, 'containerId');
    const previewUrl =
      readStringMetadata(metadata, 'preview_url') ?? readStringMetadata(metadata, 'previewUrl');
    if (bagId || containerId || previewUrl) {
      return { bagId, containerId, previewUrl };
    }
  }

  const latestTaskWithOutputBag = [...tasks]
    .reverse()
    .find((task) => (task.output_bag_ids?.length ?? 0) > 0);

  return {
    bagId: latestTaskWithOutputBag?.output_bag_ids?.[0],
    containerId: gitBranches?.merge?.container_id || gitBranches?.container_id,
  };
}

function readStringMetadata(
  metadata: DevflowSessionMessageView['metadata'] | undefined | null,
  key: string,
): string | undefined {
  const value = metadata?.[key];
  return typeof value === 'string' && value.trim() ? value.trim() : undefined;
}

export default RunWorkspacePage;
