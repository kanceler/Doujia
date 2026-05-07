/* 前后端共享的类型写在这里 */

// ============ Run ============

export type RunStatus = 'pending' | 'running' | 'success' | 'failed' | 'rejected' | 'recovered';

export interface RunConfig {
  projectId: string;
  projectName: string;
  modelProvider: string;
  modelName: string;
  apiKey: string;
  baseUrl: string;
  apiStyle: 'chat_completions' | 'responses';
  targetRepo: string;
  templateId: string;
  enableWebInject: boolean;
  enableObservability: boolean;
}

export interface RunItem {
  id: string;
  name: string;
  status: RunStatus;
  demandSummary: string | null;
  config: RunConfig | null;
  ref: string | null;
  isDemo: boolean | null;
  createdAt: string;
  updatedAt: string;
}

export interface DemoRunItem {
  id: string;
  name: string;
  description: string;
  status: RunStatus;
  createdAt: string;
}

export interface CreateRunRequest {
  demandSummary: string;
  config: RunConfig;
  templateId: string;
}

export interface ValidateConfigRequest {
  config: RunConfig;
}

export interface ValidateConfigResponse {
  valid: boolean;
  errors: string[];
}

export interface ApproveRequest {
  nodeId: string;
  comment?: string;
}

export interface RetryRequest {
  nodeId: string;
  action: 'reject' | 'retry';
  comment?: string;
}

export interface CloneRunRequest {
  newName: string;
  configOverrides?: Partial<RunConfig>;
}

// ============ Message ============

export type MessageRole = 'user' | 'assistant' | 'system';
export type MessageType =
  | 'text'
  | 'approval'
  | 'diff'
  | 'test-result'
  | 'mr-summary'
  | 'requirement-summary'
  | 'project-generated';

export interface MessageMetadata {
  approvalStatus?: string;
  diffContent?: string;
  testResults?: Array<{ name: string; passed: boolean }>;
  requirementSummary?: {
    confirmActionPath?: string;
    confirmActionLabel?: string;
    reviseActionLabel?: string;
    messageId?: string;
    confirmed?: boolean;
    onConfirm?: () => Promise<void> | void;
  };
  projectGenerated?: {
    title?: string;
    description?: string;
    actionLabel?: string;
    targetTabId?: string;
    previewUrl?: string;
    artifactId?: string;
    bagId?: string;
    containerId?: string;
  };
}

export interface MessageItem {
  id: string;
  runId: string;
  role: MessageRole;
  content: string;
  type: MessageType;
  metadata: MessageMetadata | null;
  createdAt: string;
}

export interface SendMessageRequest {
  sessionId: string;
  content: string;
}

export interface MessageListResponse {
  items: MessageItem[];
  nextCursor: string | null;
  hasMore: boolean;
}

// ============ Pipeline Node ============

export type PipelineNodeType = 'task' | 'approval' | 'sub-pipeline';
export type PipelineNodeStatus = 'pending' | 'running' | 'success' | 'failed' | 'rejected' | 'recovered';

export interface NodeSnapshot {
  agentName: string;
  input: object;
  output: object;
  timestamp: string;
}

export interface NodeArtifact {
  type: string;
  path: string;
  description: string;
}

export interface PipelineNodeItem {
  id: string;
  runId: string;
  name: string;
  type: PipelineNodeType;
  status: PipelineNodeStatus;
  parentId: string | null;
  position: number;
  ref: string | null;
  createdAt: string;
}

export interface PipelineNodeDetail {
  id: string;
  name: string;
  type: PipelineNodeType;
  status: PipelineNodeStatus;
  snapshot: NodeSnapshot | null;
  artifact: NodeArtifact | null;
  approvalRecords: object[];
  ref: string | null;
  createdAt: string;
}

// ============ Pipeline Template ============

export interface PipelineStep {
  name: string;
  type: string;
  config: object;
}

export interface PipelineDsl {
  steps: PipelineStep[];
}

export interface PipelineTemplateItem {
  id: string;
  name: string;
  description: string | null;
  isDefault: boolean | null;
}

export interface PipelineTemplateDsl {
  dsl: PipelineDsl;
  description: string;
}

// ============ Registration State ============

export type RegistrationType = 'handler' | 'op' | 'agent' | 'pipeline';
export type RegistrationStatus = 'registered' | 'missing' | 'error';

export interface RegistrationStateItem {
  id: string;
  type: RegistrationType;
  name: string;
  status: RegistrationStatus;
  errorMessage: string | null;
}

// ============ Web Inject ============

export interface PreviewResponse {
  html: string;
  baseUrl: string;
}

export interface ModifyRequest {
  elementSelector: string;
  elementContent: string;
  modifyInstruction: string;
}

export interface ModifyResponse {
  success: boolean;
  modifiedHtml: string;
  diff: string;
}

export interface SubmitMrRequest {
  mrTitle: string;
  mrDescription: string;
  diff: string;
}

export interface SubmitMrResponse {
  success: boolean;
  mrUrl: string;
}

// ============ Pipeline History ============

export interface PipelineHistoryNode {
  id: string;
  name: string;
  type: PipelineNodeType;
  status: PipelineNodeStatus;
  position: number;
  parentId: string | null;
  ref: string | null;
}

export interface PipelineHistoryEdge {
  from: string;
  to: string;
}

export interface RefHistory {
  ref: string;
  nodeId: string;
  timestamp: string;
}

export interface PipelineHistoryResponse {
  nodes: PipelineHistoryNode[];
  edges: PipelineHistoryEdge[];
  refHistory: RefHistory[];
}

// ============ Config ============

export interface ModelConfig {
  modelProvider: string;
  modelName: string;
  apiKey: string;
  baseUrl: string;
  apiStyle: 'chat_completions' | 'responses';
}

export interface ApiConfig {
  targetRepo: string;
  webhookUrl: string;
}

export interface SaveConfigRequest {
  modelConfig: ModelConfig;
  apiConfig: ApiConfig;
}
