import type { ReactNode } from 'react';

export type PipelineTaskStatus =
  | 'pending'
  | 'running'
  | 'completed'
  | 'failed'
  | 'kbug'
  | 'skipped';

export type PipelineEdgeStatus =
  | 'normal'
  | 'active'
  | 'blocked'
  | 'completed';

export interface PipelineTaskNode {
  id: string;
  title: string;
  stageId?: string;
  role: string;
  op: string;
  status: PipelineTaskStatus;
  rawStatus?: string;
  durationMs?: number;
  startedAt?: string;
  finishedAt?: string;
  artifactCount?: number;
  snapshotCount?: number;
  group?: string;
  taskId?: string;
  childPipelineInstanceId?: string;
  inputArtifacts?: string[];
  outputArtifacts?: string[];
  errorReason?: string;
}

export interface PipelineTaskEdge {
  id: string;
  source: string;
  target: string;
  status?: PipelineEdgeStatus;
}

export interface PipelineGraphData {
  nodes: PipelineTaskNode[];
  edges: PipelineTaskEdge[];
}

export interface LayoutedPipelineTaskNode extends PipelineTaskNode {
  position: {
    x: number;
    y: number;
  };
}

export interface PipelineLayoutResult {
  nodes: LayoutedPipelineTaskNode[];
  edges: PipelineTaskEdge[];
}

export interface PipelineGraphSummary {
  total: number;
  completed: number;
  running: number;
  failed: number;
  skipped: number;
  kbug: number;
  pending: number;
  totalDurationMs?: number;
}

export interface PipelineNodeActionHandlers {
  onOpenPipeline?: (instanceId: string, title: string) => void;
  onOpenTaskSnapshots?: (taskId: string, title: string) => void;
}

export interface PipelineNodeViewData extends PipelineNodeActionHandlers {
  [key: string]: unknown;
  node: PipelineTaskNode;
  selected?: boolean;
  onSelect?: (nodeId: string) => void;
}

export interface PipelineIconConfig {
  icon: ReactNode;
  toneClassName: string;
}
