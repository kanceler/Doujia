import type {
  DevflowAcceptanceCheckpointView,
  DevflowApproveCheckpointRequest,
  DevflowArtifactContentView,
  DevflowCheckpointView,
  DevflowCreateProjectRequest,
  DevflowCreateImprovementItemRequest,
  DevflowConfirmRequirementsResult,
  DevflowContinueAcceptanceRequest,
  DevflowCreateRunRequest,
  DevflowEventView,
  DevflowFinalResultOpenRequest,
  DevflowFinalResultOpenResult,
  DevflowGitBranchesView,
  DevflowImprovementItemView,
  DevflowNodeDetailView,
  DevflowPipelineGraphView,
  DevflowPipelineWorkspaceView,
  DevflowPluginRegistryStateView,
  DevflowPluginValidationResultView,
  DevflowPostSessionMessageAccepted,
  DevflowPostSessionMessageRequest,
  DevflowProjectView,
  DevflowPreviewBrowserOpenRequest,
  DevflowPreviewBrowserOpenResult,
  DevflowPreviewRepairPrepareRequest,
  DevflowPreviewRepairPrepareResult,
  DevflowPreviewScriptResult,
  DevflowPreviewStartResult,
  DevflowPreviewSubmitEditsRequest,
  DevflowPreviewSubmitEditsResult,
  DevflowRejectCheckpointRequest,
  DevflowResumeFromRefRequest,
  DevflowResumeFromRefResult,
  DevflowRunView,
  DevflowRunIterationView,
  DevflowSessionMessageView,
  DevflowWorkspaceOverviewView,
  DevflowPatchImprovementItemRequest,
  DevflowTaskFeedbackRequest,
  DevflowTaskView,
} from '@shared/devflow-api';
import { filterVisibleRecentRuns } from './recent-runs';

const DEFAULT_API_BASE_URL = 'http://127.0.0.1:18080';

function apiBaseUrl(): string {
  const configured = import.meta.env.VITE_DEVFLOW_API_BASE_URL as string | undefined;
  return (configured?.trim() || DEFAULT_API_BASE_URL).replace(/\/$/, '');
}

async function devflowFetch<T>(path: string, init?: RequestInit): Promise<T> {
  const response = await fetch(`${apiBaseUrl()}${path}`, {
    ...init,
    headers: {
      'Content-Type': 'application/json',
      ...(init?.headers ?? {}),
    },
  });
  if (!response.ok) {
    let message = `DevFlow API ${response.status}`;
    try {
      const body = (await response.json()) as { error?: string };
      if (body.error) {
        message = body.error;
      }
    } catch {
      // keep status-based message
    }
    throw new Error(message);
  }
  return response.json() as Promise<T>;
}

async function devflowUpload<T>(path: string, file: File): Promise<T> {
  const form = new FormData();
  form.append('file', file);
  const response = await fetch(`${apiBaseUrl()}${path}`, {
    method: 'POST',
    body: form,
  });
  if (!response.ok) {
    let message = `DevFlow API ${response.status}`;
    try {
      const body = (await response.json()) as { error?: string };
      if (body.error) {
        message = body.error;
      }
    } catch {
      // keep status-based message
    }
    throw new Error(message);
  }
  return response.json() as Promise<T>;
}

type DevflowPreviewHandlerName =
  | 'preview_start'
  | 'preview_browser_open'
  | 'preview_inspector'
  | 'preview_console'
  | 'preview_submit_edits'
  | 'preview_repair_prepare'
  | 'final_result_open';

function previewHandlerPath(runId: string, handler: DevflowPreviewHandlerName): string {
  const configured = import.meta.env.VITE_DEVFLOW_PREVIEW_HANDLER_PATH as string | undefined;
  const template =
    configured?.trim() ||
    '/api/runs/:runId/preview/handlers/:handler';
  return template
    .replace(':runId', encodeURIComponent(runId))
    .replace(':handler', encodeURIComponent(handler));
}

function unwrapDevflowPreviewHandlerResponse<TResponse>(payload: unknown): TResponse {
  if (
    payload &&
    typeof payload === 'object' &&
    'data' in payload &&
    (payload as { data?: unknown }).data &&
    typeof (payload as { data?: unknown }).data === 'object'
  ) {
    return (payload as { data: TResponse }).data;
  }
  return payload as TResponse;
}

async function callDevflowPreviewHandler<TResponse>(
  runId: string,
  handler: DevflowPreviewHandlerName,
  args: Record<string, unknown>,
): Promise<TResponse> {
  const payload = await devflowFetch<unknown>(previewHandlerPath(runId, handler), {
    method: 'POST',
    body: JSON.stringify({ args }),
  });
  return unwrapDevflowPreviewHandlerResponse<TResponse>(payload);
}

export async function getDevflowRuns(): Promise<{ items: DevflowRunView[] }> {
  return devflowFetch('/api/runs');
}

export async function getDevflowDemoRuns(): Promise<{ items: DevflowRunView[] }> {
  const result = await devflowFetch<{ items: DevflowRunView[] }>('/api/demo-runs');
  return { items: filterVisibleRecentRuns(result.items) };
}

export async function createDevflowRun(
  data: DevflowCreateRunRequest,
): Promise<DevflowRunView> {
  return devflowFetch('/api/runs', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function getDevflowProjects(): Promise<{ items: DevflowProjectView[] }> {
  return devflowFetch('/api/projects');
}

export async function createDevflowProject(
  data: DevflowCreateProjectRequest,
): Promise<DevflowProjectView> {
  return devflowFetch('/api/projects', {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function startDevflowRun(runId: string): Promise<DevflowRunView> {
  return devflowFetch(`/api/runs/${encodeURIComponent(runId)}/start`, {
    method: 'POST',
  });
}

export async function getDevflowRun(runId: string): Promise<DevflowRunView> {
  return devflowFetch(`/api/runs/${encodeURIComponent(runId)}`);
}

export async function getDevflowTasks(runId: string): Promise<{ items: DevflowTaskView[] }> {
  return devflowFetch(`/api/runs/${encodeURIComponent(runId)}/tasks`);
}

export async function getDevflowSessionMessages(
  runId: string,
): Promise<{ items: DevflowSessionMessageView[] }> {
  return devflowFetch(`/api/runs/${encodeURIComponent(runId)}/session/messages`);
}

export async function postDevflowSessionMessage(
  runId: string,
  data: DevflowPostSessionMessageRequest,
): Promise<DevflowPostSessionMessageAccepted> {
  return devflowFetch(`/api/runs/${encodeURIComponent(runId)}/session/messages`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export interface DevflowSessionMessageStreamHandlers {
  onDelta?: (delta: string) => void;
  onDone?: (payload: DevflowPostSessionMessageAccepted) => void;
}

export async function postDevflowSessionMessageStream(
  runId: string,
  data: DevflowPostSessionMessageRequest,
  handlers: DevflowSessionMessageStreamHandlers = {},
): Promise<void> {
  const response = await fetch(
    `${apiBaseUrl()}/api/runs/${encodeURIComponent(runId)}/session/messages/stream`,
    {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
      },
      body: JSON.stringify(data),
    },
  );
  if (!response.ok) {
    let message = `DevFlow API ${response.status}`;
    try {
      const body = (await response.json()) as { error?: string };
      if (body.error) {
        message = body.error;
      }
    } catch {
      // keep status-based message
    }
    throw new Error(message);
  }
  if (!response.body) {
    throw new Error('DevFlow stream response is empty');
  }

  const reader = response.body.getReader();
  const decoder = new TextDecoder();
  let buffer = '';
  for (;;) {
    const { done, value } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    const parts = buffer.split(/\r?\n\r?\n/);
    buffer = parts.pop() ?? '';
    for (const part of parts) {
      handleDevflowStreamEvent(part, handlers);
    }
  }
  buffer += decoder.decode();
  if (buffer.trim()) {
    handleDevflowStreamEvent(buffer, handlers);
  }
}

function handleDevflowStreamEvent(
  block: string,
  handlers: DevflowSessionMessageStreamHandlers,
): void {
  let event = 'message';
  const dataLines: string[] = [];
  for (const line of block.split(/\r?\n/)) {
    if (line.startsWith('event:')) {
      event = line.slice('event:'.length).trim();
    } else if (line.startsWith('data:')) {
      dataLines.push(line.slice('data:'.length).trimStart());
    }
  }
  if (dataLines.length === 0) return;
  const payload = JSON.parse(dataLines.join('\n')) as Record<string, unknown>;
  if (event === 'delta') {
    const delta = typeof payload.delta === 'string' ? payload.delta : '';
    if (delta) {
      handlers.onDelta?.(delta);
    }
    return;
  }
  if (event === 'error') {
    const message = typeof payload.error === 'string' ? payload.error : 'DevFlow stream failed';
    throw new Error(message);
  }
  if (event === 'done') {
    handlers.onDone?.(payload as unknown as DevflowPostSessionMessageAccepted);
  }
}

export async function confirmDevflowRequirementSummary(
  runId: string,
  messageId: string,
  action: 'accept' = 'accept',
): Promise<{ confirmed: boolean; item: DevflowImprovementItemView }> {
  return devflowFetch(
    `/api/runs/${encodeURIComponent(runId)}/session/messages/${encodeURIComponent(messageId)}/confirm`,
    {
      method: 'POST',
      body: JSON.stringify({ action }),
    },
  );
}

export async function confirmDevflowRequirements(
  runId: string,
): Promise<DevflowConfirmRequirementsResult> {
  return devflowFetch(`/api/runs/${encodeURIComponent(runId)}/requirements/confirm`, {
    method: 'POST',
    body: JSON.stringify({}),
  });
}

export async function getDevflowImprovementItems(
  runId: string,
): Promise<{ items: DevflowImprovementItemView[] }> {
  return devflowFetch(`/api/runs/${encodeURIComponent(runId)}/improvement-items`);
}

export async function createDevflowImprovementItem(
  runId: string,
  data: DevflowCreateImprovementItemRequest,
): Promise<DevflowImprovementItemView> {
  return devflowFetch(`/api/runs/${encodeURIComponent(runId)}/improvement-items`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function patchDevflowImprovementItem(
  runId: string,
  itemId: string,
  data: DevflowPatchImprovementItemRequest,
): Promise<DevflowImprovementItemView> {
  return devflowFetch(
    `/api/runs/${encodeURIComponent(runId)}/improvement-items/${encodeURIComponent(itemId)}`,
    {
      method: 'PATCH',
      body: JSON.stringify(data),
    },
  );
}

export async function removeDevflowImprovementItem(
  runId: string,
  itemId: string,
): Promise<{ removed: boolean; item: DevflowImprovementItemView }> {
  return devflowFetch(
    `/api/runs/${encodeURIComponent(runId)}/improvement-items/${encodeURIComponent(itemId)}`,
    {
      method: 'DELETE',
    },
  );
}

export async function getDevflowIterations(
  runId: string,
): Promise<{ items: DevflowRunIterationView[] }> {
  return devflowFetch(`/api/runs/${encodeURIComponent(runId)}/iterations`);
}

export async function getDevflowWorkspaceOverview(
  runId: string,
): Promise<DevflowWorkspaceOverviewView> {
  return devflowFetch(`/api/runs/${encodeURIComponent(runId)}/workspace-overview`);
}

export async function getDevflowPipelineWorkspace(
  runId: string,
  focusInstanceId?: string,
): Promise<DevflowPipelineWorkspaceView> {
  const params = new URLSearchParams();
  if (focusInstanceId?.trim()) {
    params.set('focus_instance_id', focusInstanceId.trim());
  }
  const suffix = params.toString() ? `?${params.toString()}` : '';
  return devflowFetch(`/api/runs/${encodeURIComponent(runId)}/pipeline-workspace${suffix}`);
}

export async function getDevflowAcceptanceCheckpoint(
  runId: string,
): Promise<DevflowAcceptanceCheckpointView> {
  return devflowFetch(`/api/runs/${encodeURIComponent(runId)}/acceptance-checkpoint`);
}

export async function approveDevflowAcceptanceCheckpoint(
  runId: string,
  data: DevflowApproveCheckpointRequest = {},
): Promise<DevflowRunView> {
  return devflowFetch(`/api/runs/${encodeURIComponent(runId)}/acceptance-checkpoint/approve`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function continueDevflowAcceptanceCheckpoint(
  runId: string,
  data: DevflowContinueAcceptanceRequest = {},
): Promise<DevflowRunView> {
  return devflowFetch(`/api/runs/${encodeURIComponent(runId)}/acceptance-checkpoint/continue`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function getDevflowCheckpoints(
  runId: string,
): Promise<{ items: DevflowCheckpointView[] }> {
  return devflowFetch(`/api/runs/${encodeURIComponent(runId)}/checkpoints`);
}

export async function getDevflowCheckpoint(
  runId: string,
  taskId: string,
): Promise<DevflowCheckpointView> {
  return devflowFetch(
    `/api/runs/${encodeURIComponent(runId)}/checkpoints/${encodeURIComponent(taskId)}`,
  );
}

export async function sendDevflowTaskFeedback(
  runId: string,
  taskId: string,
  data: DevflowTaskFeedbackRequest,
): Promise<DevflowCheckpointView> {
  return devflowFetch(
    `/api/runs/${encodeURIComponent(runId)}/tasks/${encodeURIComponent(taskId)}/feedback`,
    {
      method: 'POST',
      body: JSON.stringify(data),
    },
  );
}

export async function approveDevflowCheckpoint(
  runId: string,
  taskId: string,
  data: DevflowApproveCheckpointRequest = {},
): Promise<DevflowCheckpointView> {
  return devflowFetch(
    `/api/runs/${encodeURIComponent(runId)}/checkpoints/${encodeURIComponent(taskId)}/approve`,
    {
      method: 'POST',
      body: JSON.stringify(data),
    },
  );
}

export async function rejectDevflowCheckpoint(
  runId: string,
  taskId: string,
  data: DevflowRejectCheckpointRequest,
): Promise<DevflowCheckpointView> {
  return devflowFetch(
    `/api/runs/${encodeURIComponent(runId)}/checkpoints/${encodeURIComponent(taskId)}/reject`,
    {
      method: 'POST',
      body: JSON.stringify(data),
    },
  );
}

export async function resumeDevflowFromRef(
  runId: string,
  data: DevflowResumeFromRefRequest = {},
): Promise<DevflowResumeFromRefResult> {
  return devflowFetch(`/api/runs/${encodeURIComponent(runId)}/resume-from-ref`, {
    method: 'POST',
    body: JSON.stringify(data),
  });
}

export async function getDevflowArtifactContent(
  runId: string,
  artifactId: string,
): Promise<DevflowArtifactContentView> {
  return devflowFetch(
    `/api/runs/${encodeURIComponent(runId)}/artifacts/${encodeURIComponent(artifactId)}/content`,
  );
}

export async function getDevflowEvents(runId: string): Promise<{ items: DevflowEventView[] }> {
  return devflowFetch(`/api/runs/${encodeURIComponent(runId)}/events`);
}

export async function getDevflowPipelineGraph(
  runId: string,
  refName = 'main',
): Promise<DevflowPipelineGraphView> {
  const params = new URLSearchParams({ ref: refName });
  return devflowFetch(`/api/runs/${encodeURIComponent(runId)}/pipeline-graph?${params}`);
}

export async function getDevflowGitBranches(runId: string): Promise<DevflowGitBranchesView> {
  return devflowFetch(`/api/runs/${encodeURIComponent(runId)}/git-branches`);
}

export async function getDevflowNodeDetail(
  runId: string,
  taskId: string,
): Promise<DevflowNodeDetailView> {
  return devflowFetch(
    `/api/runs/${encodeURIComponent(runId)}/nodes/${encodeURIComponent(taskId)}`,
  );
}

export async function getDevflowPluginRegistryState(): Promise<DevflowPluginRegistryStateView> {
  return devflowFetch('/api/plugins/registry-state');
}

export async function uploadDevflowPluginPack(file: File): Promise<{ job_id: string }> {
  return devflowUpload('/api/plugins/upload-pack', file);
}

export async function uploadDevflowPipeline(file: File): Promise<{ job_id: string }> {
  return devflowUpload('/api/plugins/upload-pipeline', file);
}

export async function getDevflowPluginValidationResult(
  jobId: string,
): Promise<DevflowPluginValidationResultView> {
  return devflowFetch(`/api/plugins/validations/${encodeURIComponent(jobId)}`);
}

export async function callDevflowPreviewStart(
  runId: string,
): Promise<DevflowPreviewStartResult> {
  return callDevflowPreviewHandler(runId, 'preview_start', {});
}

export async function callDevflowFinalResultOpen(
  runId: string,
  data: DevflowFinalResultOpenRequest,
): Promise<DevflowFinalResultOpenResult> {
  return callDevflowPreviewHandler(runId, 'final_result_open', { ...data });
}

export async function callDevflowPreviewBrowserOpen(
  runId: string,
  data: DevflowPreviewBrowserOpenRequest,
): Promise<DevflowPreviewBrowserOpenResult> {
  return callDevflowPreviewHandler(runId, 'preview_browser_open', { ...data });
}

export async function callDevflowPreviewInspector(
  runId: string,
): Promise<DevflowPreviewScriptResult> {
  return callDevflowPreviewHandler(runId, 'preview_inspector', {});
}

export async function callDevflowPreviewConsole(
  runId: string,
): Promise<DevflowPreviewScriptResult> {
  return callDevflowPreviewHandler(runId, 'preview_console', {});
}

export async function callDevflowPreviewSubmitEdits(
  runId: string,
  data: DevflowPreviewSubmitEditsRequest,
): Promise<DevflowPreviewSubmitEditsResult> {
  return callDevflowPreviewHandler(runId, 'preview_submit_edits', {
    ...data,
  });
}

export async function callDevflowPreviewRepairPrepare(
  runId: string,
  data: DevflowPreviewRepairPrepareRequest,
): Promise<DevflowPreviewRepairPrepareResult> {
  return callDevflowPreviewHandler(runId, 'preview_repair_prepare', {
    ...data,
  });
}
