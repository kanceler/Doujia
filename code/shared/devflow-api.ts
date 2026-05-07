export type GoRunStatus =
  | 'created'
  | 'running'
  | 'awaiting_acceptance'
  | 'completed'
  | 'failed';

export type GoTaskStatus =
  | 'pending'
  | 'waiting_external'
  | 'dispatched'
  | 'running'
  | 'done'
  | 'blocked'
  | 'failed';

export type GoTaskResultCode =
  | ''
  | 'kok'
  | 'kfail'
  | 'krewrite'
  | 'kreplan'
  | 'kbug'
  | 'kcontrol_invalid';

export interface DevflowRunView {
  run_id: string;
  pipeline_id: string;
  status: GoRunStatus;
  project_id?: string;
  project_dir: string;
  session_id?: string;
  current_iteration_no: number;
  latest_delivery_frontier_id?: string;
  latest_acceptance_checkpoint_task_id?: string;
  config: {
    delivery?: {
      max_coder_agents?: number;
      max_tester_agents?: number;
      require_tester_per_module?: boolean;
      allow_parallel_work?: boolean;
      global_verify_commands?: string[];
      global_test_timeout_seconds?: number;
      git?: {
        repo_url?: string;
        main_branch?: string;
        base_ref?: string;
      };
    };
    llm?: {
      provider?: string;
      model?: string;
      api_key?: string;
      base_url?: string;
      api_style?: string;
    };
  };
  created_at: string;
  updated_at: string;
}

export interface DevflowCreateRunRequest {
  run_id?: string;
  demand_summary?: string;
  project_id?: string;
  pipeline_id?: string;
  target_repo?: string;
  main_branch?: string;
  base_ref?: string;
  model_provider?: string;
  model_name?: string;
  api_key?: string;
  base_url?: string;
  api_style?: string;
  start_immediately?: boolean;
}

export interface DevflowProjectView {
  project_id: string;
  name: string;
  llm: {
    provider?: string;
    model?: string;
    api_key?: string;
    base_url?: string;
    api_style?: string;
  };
  created_at: string;
  updated_at: string;
}

export interface DevflowCreateProjectRequest {
  name: string;
  llm: {
    provider?: string;
    model?: string;
    api_key?: string;
    base_url?: string;
    api_style?: string;
  };
}

export interface DevflowTaskView {
  task_id: string;
  run_id: string;
  pipeline_instance_id?: string;
  stage_id: string;
  agent_role: string;
  agent_id: string;
  op: string;
  status: GoTaskStatus;
  result?: GoTaskResultCode;
  error_message?: string;
  input_bag_ids?: string[];
  output_bag_ids?: string[];
  created_at: string;
  updated_at: string;
}

export interface DevflowEventView {
  event_id: string;
  run_id: string;
  task_id?: string;
  agent_id?: string;
  type: string;
  message: string;
  payload_json?: string;
  created_at: string;
}

export interface DevflowRefView {
  ref_name: string;
  frontier_snapshot_id?: string;
  frontier_snapshot_ids: string[];
  updated_at: string;
}

export interface DevflowObjectView {
  object_id: string;
  object_type: string;
  storage_uri?: string;
  created_at: string;
}

export interface DevflowVersionView {
  artifact_version_id: string;
  logical_artifact_id: string;
  logical_artifact: {
    logical_artifact_id: string;
    run_id: string;
    namespace: string;
    logical_key: string;
    created_at: string;
  };
  object_ids: string[];
  objects?: DevflowObjectView[];
  created_at: string;
}

export interface DevflowBagView {
  bag_id: string;
  run_id: string;
  artifact_version_ids: string[];
  versions?: DevflowVersionView[];
  producer_snapshot_id?: string;
  consumer_snapshot_ids?: string[];
  created_at: string;
}

export interface DevflowSnapshotView {
  snapshot_id: string;
  run_id: string;
  task_id: string;
  pipeline_instance_id?: string;
  transition_id?: string;
  agent_role?: string;
  agent_id?: string;
  op?: string;
  result: GoTaskResultCode;
  input_bag_ids?: string[];
  output_bag_ids?: string[];
  input_bags?: DevflowBagView[];
  output_bags?: DevflowBagView[];
  diagnostics_json?: string;
  runtime_context_json?: string;
  created_at: string;
}

export interface DevflowFrontierSnapshotView {
  frontier_snapshot_id: string;
  run_id: string;
  parent_frontier_snapshot_ids?: string[];
  task_snapshot_ids: string[];
  created_by_mode?: string;
  created_by_event_id?: string;
  details_json?: string;
  created_at: string;
}

export interface DevflowRefMoveEventView {
  event_id: string;
  run_id: string;
  ref_name: string;
  from_frontier_snapshot_ids?: string[];
  to_frontier_snapshot_ids: string[];
  mode: string;
  reason?: string;
  details_json?: string;
  created_at: string;
}

export interface DevflowPipelineGraphView {
  run_id: string;
  ref: DevflowRefView;
  refs?: DevflowRefView[];
  frontier_snapshots?: DevflowFrontierSnapshotView[];
  ref_move_events?: DevflowRefMoveEventView[];
  snapshots: DevflowSnapshotView[];
  bags?: DevflowBagView[];
  history_snapshots?: DevflowSnapshotView[];
  history_bags?: DevflowBagView[];
  history_frontier_snapshots?: DevflowFrontierSnapshotView[];
}

export interface DevflowGitBranchesWarningView {
  task_id?: string;
  bag_id?: string;
  logical_key?: string;
  message: string;
}

export interface DevflowGitModuleBranchView {
  task_id?: string;
  agent_id?: string;
  snapshot_id?: string;
  bag_id?: string;
  logical_key?: string;
  created_at?: string;
  module_id?: string;
  module_name?: string;
  branch?: string;
  commit?: string;
  base_branch?: string;
  base_commit?: string;
  container_id?: string;
  changed_files?: string[];
  result?: string;
  test_passed?: boolean;
}

export interface DevflowGitMergeView {
  task_id?: string;
  agent_id?: string;
  snapshot_id?: string;
  bag_id?: string;
  logical_key?: string;
  created_at?: string;
  base_branch?: string;
  base_commit?: string;
  container_id?: string;
  merged_commit?: string;
  applied_commits?: string[];
  modules?: DevflowGitModuleBranchView[];
  result?: string;
}

export interface DevflowGitBranchesView {
  run_id: string;
  base_branch?: string;
  base_commit?: string;
  container_id?: string;
  module_branches: DevflowGitModuleBranchView[];
  merge?: DevflowGitMergeView;
  warnings?: DevflowGitBranchesWarningView[];
}

export interface DevflowArtifactRecord {
  ID?: string;
  RunID?: string;
  TaskID?: string;
  AgentID?: string;
  Kind?: string;
  URI?: string;
  CreatedAt?: string;
  id?: string;
  run_id?: string;
  task_id?: string;
  agent_id?: string;
  kind?: string;
  uri?: string;
  created_at?: string;
}

export interface DevflowCheckpointView {
  checkpoint_id: string;
  task_id: string;
  run_id: string;
  stage_id: string;
  op: string;
  status: GoTaskStatus;
  result?: GoTaskResultCode;
  agent_role: string;
  agent_id: string;
  title: string;
  summary?: string;
  artifact_refs?: DevflowArtifactRecord[];
  input_bag_ids?: string[];
  output_bag_ids?: string[];
  can_approve: boolean;
  can_reject: boolean;
  created_at: string;
  updated_at: string;
}

export interface DevflowTaskFeedbackRequest {
  result: GoTaskResultCode;
  message?: string;
  agent_id?: string;
  op?: string;
  artifact_uris?: string[];
  input_bag_ids?: string[];
  output_bag_ids?: string[];
  outputs?: Array<Record<string, unknown>>;
  produced_bags?: Array<Record<string, unknown>>;
  control?: Array<Record<string, unknown>>;
  commit?: Record<string, unknown>;
}

export interface DevflowApproveCheckpointRequest {
  comment?: string;
}

export interface DevflowRejectCheckpointRequest {
  reason: string;
  mode?: 'rewrite' | 'replan' | 'bugfix';
}

export interface DevflowResumeFromRefRequest {
  ref_name?: string;
}

export interface DevflowResumeFromRefResult {
  run_id: string;
  ref_name: string;
  frontier_snapshot_id?: string;
  materialized_tasks: number;
  materialized_instances: number;
  dispatched_tasks: number;
  run_status: GoRunStatus;
}

export interface DevflowArtifactContentView {
  artifact_id?: string;
  uri: string;
  content_type: string;
  text?: string;
  size: number;
  truncated: boolean;
}

export interface DevflowNodeDetailView {
  task: DevflowTaskView;
  snapshot?: DevflowSnapshotView;
  events: DevflowEventView[];
  artifacts: DevflowArtifactRecord[];
}

export interface DevflowSessionMessageView {
  message_id: string;
  run_id: string;
  iteration_no: number;
  role: string;
  message_type: string;
  content: string;
  metadata?: {
    kind?: string;
    confirm_action_path?: string;
    confirm_action_label?: string;
    revise_action_label?: string;
    summary_key?: string;
    [key: string]: unknown;
  } | null;
  created_at: string;
}

export interface DevflowPostSessionMessageRequest {
  content: string;
  message_type?: string;
}

export interface DevflowPostSessionMessageAccepted {
  accepted: boolean;
  message_id: string;
  iteration_no: number;
  message_type: string;
}

export interface DevflowConfirmRequirementsResult {
  run_id: string;
  pipeline_id: string;
  status: GoRunStatus;
  project_id?: string;
  project_dir: string;
  session_id?: string;
  current_iteration_no: number;
  latest_delivery_frontier_id?: string;
  latest_acceptance_checkpoint_task_id?: string;
  config: DevflowRunView['config'];
  created_at: string;
  updated_at: string;
}

export interface DevflowRunIterationView {
  run_id: string;
  iteration_no: number;
  start_frontier_id?: string;
  delivery_frontier_id?: string;
  acceptance_checkpoint_task_id?: string;
  status: GoRunStatus;
  created_at: string;
  updated_at: string;
}

export interface DevflowSessionArtifactView {
  artifact_id: string;
  run_id: string;
  iteration_no: number;
  kind: string;
  title: string;
  content: string;
  created_at: string;
}

export interface DevflowImprovementItemView {
  item_id: string;
  run_id: string;
  iteration_no: number;
  title: string;
  detail: string;
  source: string;
  status: string;
  created_at: string;
  updated_at: string;
}

export interface DevflowCreateImprovementItemRequest {
  title: string;
  detail?: string;
  source?: string;
}

export interface DevflowPatchImprovementItemRequest {
  title?: string;
  detail?: string;
  source?: string;
  status?: string;
}

export interface DevflowAcceptanceCheckpointView {
  run_id: string;
  checkpoint_task_id: string;
  current_iteration_no: number;
  latest_delivery_frontier_id?: string;
  status: GoRunStatus;
}

export interface DevflowContinueAcceptanceRequest {
  selected_item_ids?: string[];
  freeform_text?: string;
}

export interface DevflowWorkspaceOverviewView {
  run: DevflowRunView;
  current_iteration_no: number;
  acceptance_checkpoint: DevflowAcceptanceCheckpointView;
  iterations: DevflowRunIterationView[];
  session_artifacts: DevflowSessionArtifactView[];
  open_improvement_items: DevflowImprovementItemView[];
}

export type DevflowPipelineWorkspaceStatus =
  | 'not_started'
  | 'running'
  | 'completed'
  | 'waiting_human'
  | 'failed'
  | 'recovered';

export interface DevflowPipelineWorkspaceTaskView {
  task_id: string;
  stage_id: string;
  op: string;
  status: DevflowPipelineWorkspaceStatus;
}

export interface DevflowPipelineWorkspaceInstanceView {
  instance_id: string;
  pipeline_id: string;
  parent_instance_id?: string;
  parent_transition_id?: string;
  instance_key?: string;
  status: DevflowPipelineWorkspaceStatus;
  default_collapsed: boolean;
  tasks: DevflowPipelineWorkspaceTaskView[];
}

export interface DevflowPipelineWorkspaceEdgeView {
  from_instance_id: string;
  to_instance_id: string;
  kind: string;
  label?: string;
}

export interface DevflowPipelineWorkspaceMainNodeView {
  node_id: string;
  node_kind?: string;
  stage_id: string;
  label: string;
  status: DevflowPipelineWorkspaceStatus;
  task_id?: string;
  child_pipeline_instance_id?: string;
}

export interface DevflowPipelineWorkspaceMainEdgeView {
  from_node_id: string;
  to_node_id: string;
  kind: string;
  label?: string;
}

export interface DevflowPipelineWorkspaceView {
  run_id: string;
  status: GoRunStatus;
  current_iteration_no: number;
  iterations: DevflowRunIterationView[];
  instances: DevflowPipelineWorkspaceInstanceView[];
  edges: DevflowPipelineWorkspaceEdgeView[];
  main_pipeline_nodes?: DevflowPipelineWorkspaceMainNodeView[];
  main_pipeline_edges?: DevflowPipelineWorkspaceMainEdgeView[];
  collapsed_child_pipeline_nodes?: DevflowPipelineWorkspaceMainNodeView[];
}

export interface DevflowPluginHandlerStateView {
  handler_id: string;
  execution_driver?: string;
  impl_ref?: string;
}

export interface DevflowPluginOpStateView {
  op_id: string;
  role: string;
  op: string;
  impl_ref?: string;
}

export interface DevflowPluginRoleStateView {
  role_id: string;
  execution_driver?: string;
  driver_ref?: string;
  interaction_mode?: string;
}

export interface DevflowPluginPipelineStateView {
  pipeline_id: string;
  name?: string;
}

export interface DevflowPluginRegistryStateView {
  handlers: DevflowPluginHandlerStateView[];
  ops: DevflowPluginOpStateView[];
  roles: DevflowPluginRoleStateView[];
  pipelines: DevflowPluginPipelineStateView[];
}

export interface DevflowPluginValidationResultView {
  job_id: string;
  source_type: string;
  status: string;
  file_name: string;
  errors?: string[];
  activated_handlers?: string[];
  activated_ops?: string[];
  activated_roles?: string[];
  activated_pipelines?: string[];
  created_at: string;
  updated_at: string;
}

export interface DevflowPreviewStartResult {
  session_id: string;
  preview_url: string;
  port: number;
  container_id: string;
  module_id: string;
  workdir: string;
  manifest_path: string;
}

export interface DevflowPreviewBrowserOpenRequest {
  session_id: string;
}

export interface DevflowPreviewBrowserOpenResult {
  session_id: string;
  preview_url: string;
  injection_script: string;
  confirm_event: 'doujia:submit-edits';
  confirm_handler: 'preview_submit_edits';
}

export interface DevflowPreviewScriptResult {
  script: string;
}

export interface DevflowPreviewSelectedNode {
  selector: string;
  tag: string;
  text: string;
  attributes: Record<string, string>;
}

export type DevflowPreviewOperationType =
  | 'set_text'
  | 'set_style'
  | 'set_layout'
  | 'delete_node'
  | 'apply_variant';

export interface DevflowPreviewEditorOperation {
  type: DevflowPreviewOperationType;
  target?: string;
  property?: string;
  value?: string;
  variant?: string;
  css?: Record<string, string>;
}

export interface DevflowPreviewSubmitEditsRequest {
  session_id: string;
  selected_node: DevflowPreviewSelectedNode;
  operations: DevflowPreviewEditorOperation[];
}

export interface DevflowPreviewSubmitEditsLocalResult {
  status: 'kok';
  mode: 'local';
  next_action: 'refresh_preview';
  changed_files: string[];
  [key: string]: unknown;
}

export interface DevflowPreviewSubmitEditsAgentResult {
  status: string;
  mode: 'agent';
  next_action: string;
  repair_instruction: string;
  repair_instruction_path: string;
  repair_task: unknown;
  repair_result: unknown;
  issue_path?: string;
  [key: string]: unknown;
}

export type DevflowPreviewSubmitEditsResult =
  | DevflowPreviewSubmitEditsLocalResult
  | DevflowPreviewSubmitEditsAgentResult;

export interface DevflowPreviewRepairPrepareRequest {
  repair_instruction: string;
}

export interface DevflowPreviewRepairPrepareResult {
  repair_instruction_path: string;
  repair_task: unknown;
  repair_bundle: unknown;
}

export interface DevflowFinalResultOpenRequest {
  bag_id?: string;
  container_id?: string;
  preview_url?: string;
}

export interface DevflowFinalResultOpenResult {
  status: 'running' | 'ready' | 'failed';
  preview_url: string;
  container_id?: string;
  bag_id?: string;
  message?: string;
}
