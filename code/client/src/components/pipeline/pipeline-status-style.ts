import type { CSSProperties } from 'react';
import type { PipelineEdgeStatus, PipelineGraphSummary, PipelineTaskNode, PipelineTaskStatus } from './pipeline-types';

export const PIPELINE_STATUS_LABEL: Record<PipelineTaskStatus, string> = {
  pending: '等待中',
  running: '进行中',
  completed: '已完成',
  failed: '失败',
  kbug: '阻塞',
  skipped: '跳过',
};

export const PIPELINE_STATUS_BADGE_CLASS: Record<PipelineTaskStatus, string> = {
  pending: 'border-slate-200 bg-slate-100 text-slate-600',
  running: 'border-blue-200 bg-blue-50 text-blue-700',
  completed: 'border-emerald-200 bg-emerald-50 text-emerald-700',
  failed: 'border-red-200 bg-red-50 text-red-700',
  kbug: 'border-orange-200 bg-orange-50 text-orange-700',
  skipped: 'border-slate-200 bg-slate-50 text-slate-500',
};

export const PIPELINE_STATUS_NODE_CLASS: Record<PipelineTaskStatus, string> = {
  pending: 'border-slate-200/90 bg-white/92',
  running: 'border-blue-300/90 bg-white/94 shadow-blue-100',
  completed: 'border-emerald-200/90 bg-white/94',
  failed: 'border-red-300/90 bg-white/94 shadow-red-100',
  kbug: 'border-orange-300/90 bg-white/94 shadow-orange-100',
  skipped: 'border-slate-200/90 bg-slate-50/90 opacity-80',
};

export const PIPELINE_EDGE_COLOR: Record<PipelineEdgeStatus, string> = {
  normal: '#cbd5e1',
  active: '#3b82f6',
  blocked: '#f97316',
  completed: '#22c55e',
};

export function normalizePipelineStatus(status?: string): PipelineTaskStatus {
  switch ((status ?? '').trim().toLowerCase()) {
    case 'done':
    case 'success':
    case 'complete':
    case 'completed':
    case 'recovered':
      return 'completed';
    case 'running':
    case 'in_progress':
      return 'running';
    case 'failed':
    case 'error':
      return 'failed';
    case 'kbug':
    case 'waiting_human':
    case 'blocked':
    case 'rejected':
      return 'kbug';
    case 'skipped':
    case 'skip':
      return 'skipped';
    case 'pending':
    case 'not_started':
    default:
      return 'pending';
  }
}

export function deriveEdgeStatus(
  sourceStatus?: PipelineTaskStatus,
  targetStatus?: PipelineTaskStatus,
): PipelineEdgeStatus {
  if (sourceStatus === 'failed' || targetStatus === 'failed' || sourceStatus === 'kbug' || targetStatus === 'kbug') {
    return 'blocked';
  }
  if (targetStatus === 'running' || sourceStatus === 'running') {
    return 'active';
  }
  if (sourceStatus === 'completed' && targetStatus === 'completed') {
    return 'completed';
  }
  return 'normal';
}

export function edgeStyleForStatus(status: PipelineEdgeStatus = 'normal'): CSSProperties {
  return {
    stroke: PIPELINE_EDGE_COLOR[status],
    strokeWidth: status === 'normal' ? 1.5 : 2.25,
  };
}

export function summarizeStatus(summary: PipelineGraphSummary, fallbackStatus?: string): PipelineTaskStatus {
  if (summary.total === 0) {
    return normalizePipelineStatus(fallbackStatus);
  }
  if (summary.failed > 0) {
    return 'failed';
  }
  if (summary.kbug > 0) {
    return 'kbug';
  }
  if (summary.running > 0) {
    return 'running';
  }
  if (summary.completed > 0 && summary.completed + summary.skipped === summary.total) {
    return 'completed';
  }
  if (summary.pending > 0) {
    return 'pending';
  }
  if (summary.skipped === summary.total) {
    return 'skipped';
  }
  return normalizePipelineStatus(fallbackStatus);
}

export function formatDuration(durationMs?: number): string {
  if (!durationMs || durationMs < 0) {
    return '--';
  }
  const totalSeconds = Math.max(1, Math.round(durationMs / 1000));
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const seconds = totalSeconds % 60;
  if (hours > 0) {
    return `${hours}h ${minutes}m`;
  }
  if (minutes > 0) {
    return `${minutes}m ${seconds.toString().padStart(2, '0')}s`;
  }
  return `${seconds}s`;
}

export function summarizePipeline(nodes: PipelineTaskNode[]): PipelineGraphSummary {
  const summary: PipelineGraphSummary = {
    total: nodes.length,
    completed: 0,
    running: 0,
    failed: 0,
    skipped: 0,
    kbug: 0,
    pending: 0,
    totalDurationMs: undefined,
  };

  let totalDuration = 0;
  let hasDuration = false;
  for (const node of nodes) {
    summary[node.status] += 1;
    if (typeof node.durationMs === 'number') {
      totalDuration += node.durationMs;
      hasDuration = true;
    }
  }
  summary.totalDurationMs = hasDuration ? totalDuration : undefined;
  return summary;
}
