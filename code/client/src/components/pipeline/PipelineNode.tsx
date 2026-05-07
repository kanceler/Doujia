import { memo } from 'react';
import type { Node, NodeProps } from '@xyflow/react';
import { Handle, Position } from '@xyflow/react';
import {
  Blocks,
  Box,
  Check,
  Circle,
  CircleAlert,
  CircleSlash,
  Clock3,
  Code2,
  FileText,
  FlaskConical,
  GitMerge,
  Loader2,
  Network,
  PackageOpen,
  ScrollText,
  ShieldCheck,
  Workflow,
} from 'lucide-react';
import {
  PIPELINE_STATUS_BADGE_CLASS,
  PIPELINE_STATUS_LABEL,
  PIPELINE_STATUS_NODE_CLASS,
  formatDuration,
} from './pipeline-status-style';
import type { PipelineIconConfig, PipelineNodeViewData, PipelineTaskStatus } from './pipeline-types';

type PipelineFlowNode = Node<PipelineNodeViewData, 'pipelineTask'>;

const PipelineNodeComponent: React.FC<NodeProps<PipelineFlowNode>> = ({ data, selected }) => {
  const node = data.node;
  const iconConfig = iconForNode(node.role, node.op);
  const stageLabel = node.stageId || (node.role === 'pipeline' ? node.op : `${node.role}_${node.op}`);
  const hasSnapshotAction = Boolean(node.taskId && data.onOpenTaskSnapshots);
  const hasPipelineAction = Boolean(node.childPipelineInstanceId && data.onOpenPipeline);

  return (
    <div
      role="button"
      tabIndex={0}
      onClick={() => data.onSelect?.(node.id)}
      onKeyDown={(event) => {
        if (event.key === 'Enter' || event.key === ' ') {
          event.preventDefault();
          data.onSelect?.(node.id);
        }
      }}
      className={[
        'cursor-pointer',
        'group relative h-[108px] w-[196px] rounded-[22px] border bg-white/92 p-3 text-left shadow-[0_12px_30px_rgba(15,23,42,0.04)] transition-all duration-200 backdrop-blur',
        'hover:-translate-y-0.5 hover:shadow-[0_18px_34px_rgba(15,23,42,0.08)] focus:outline-none focus:ring-2 focus:ring-blue-300',
        selected ? 'ring-2 ring-blue-400 ring-offset-2' : '',
        PIPELINE_STATUS_NODE_CLASS[node.status],
      ].join(' ')}
    >
      <Handle type="target" position={Position.Left} className="!h-2 !w-2 !border-0 !bg-slate-300" />
      <Handle type="source" position={Position.Right} className="!h-2 !w-2 !border-0 !bg-slate-300" />

      <div className="flex items-start justify-between gap-3">
        <div className={`flex size-8 shrink-0 items-center justify-center rounded-[14px] ${iconConfig.toneClassName}`}>
          {iconConfig.icon}
        </div>
        <StatusMark status={node.status} />
      </div>

      <div className="mt-2 min-w-0">
        <div className="truncate text-[13px] font-semibold leading-4 text-slate-950">{node.title}</div>
        <div className="mt-0.5 truncate text-[10px] leading-4 text-slate-500">{stageLabel}</div>
      </div>

      <div className="mt-2 flex items-center justify-between gap-2">
        <div className="inline-flex items-center gap-1 text-[10px] text-slate-500">
          <Clock3 className="size-3" />
          <span>{formatDuration(node.durationMs)}</span>
        </div>
        <div className="flex items-center gap-1.5">
          {hasPipelineAction ? (
            <button
              type="button"
              className="nodrag rounded-full border border-slate-200 bg-slate-50 px-1.5 py-0.5 text-[9px] font-medium text-slate-700 hover:border-blue-200 hover:bg-blue-50"
              onClick={(event) => {
                event.stopPropagation();
                data.onOpenPipeline?.(node.childPipelineInstanceId!, node.title);
              }}
            >
              Sub
            </button>
          ) : null}
          {hasSnapshotAction ? (
            <button
              type="button"
              className="nodrag rounded-full border border-slate-200 bg-slate-50 px-1.5 py-0.5 text-[9px] font-medium text-slate-700 hover:border-emerald-200 hover:bg-emerald-50"
              onClick={(event) => {
                event.stopPropagation();
                data.onOpenTaskSnapshots?.(node.taskId!, node.title);
              }}
            >
              Snap
            </button>
          ) : null}
        </div>
      </div>
    </div>
  );
};

const StatusMark: React.FC<{ status: PipelineTaskStatus }> = ({ status }) => {
  if (status === 'running') {
    return (
      <span className={`inline-flex items-center gap-1 rounded-full border px-1.5 py-0.5 text-[9px] font-semibold ${PIPELINE_STATUS_BADGE_CLASS.running}`}>
        <Loader2 className="size-2.5 animate-spin" />
        {PIPELINE_STATUS_LABEL.running}
      </span>
    );
  }

  const icon = {
    completed: <Check className="size-2.5" />,
    failed: <CircleAlert className="size-2.5" />,
    kbug: <CircleAlert className="size-2.5" />,
    skipped: <CircleSlash className="size-2.5" />,
    pending: <Circle className="size-2.5" />,
  }[status];

  return (
    <span className={`inline-flex items-center gap-1 rounded-full border px-1.5 py-0.5 text-[9px] font-semibold ${PIPELINE_STATUS_BADGE_CLASS[status]}`}>
      {icon}
      {PIPELINE_STATUS_LABEL[status]}
    </span>
  );
};

function iconForNode(role: string, op: string): PipelineIconConfig {
  const key = `${role}_${op}`;
  if (key.includes('write_requirement')) {
    return { icon: <FileText className="size-4 text-emerald-700" />, toneClassName: 'bg-emerald-50' };
  }
  if (key.includes('write_plan') && role === 'pm') {
    return { icon: <ScrollText className="size-4 text-blue-700" />, toneClassName: 'bg-blue-50' };
  }
  if (key.includes('write_plan') && role === 'architect') {
    return { icon: <Workflow className="size-4 text-violet-700" />, toneClassName: 'bg-violet-50' };
  }
  if (key.includes('create_container')) {
    return { icon: <Box className="size-4 text-cyan-700" />, toneClassName: 'bg-cyan-50' };
  }
  if (key.includes('split_module') || key.includes('test_all_modules')) {
    return { icon: <Blocks className="size-4 text-orange-700" />, toneClassName: 'bg-orange-50' };
  }
  if (key.includes('write_code')) {
    return { icon: <Code2 className="size-4 text-slate-700" />, toneClassName: 'bg-slate-100' };
  }
  if (key.includes('test_code') || key.includes('test_data')) {
    return { icon: <FlaskConical className="size-4 text-rose-700" />, toneClassName: 'bg-rose-50' };
  }
  if (key.includes('merge_code')) {
    return { icon: <GitMerge className="size-4 text-indigo-700" />, toneClassName: 'bg-indigo-50' };
  }
  if (key.includes('acceptance')) {
    return { icon: <ShieldCheck className="size-4 text-emerald-700" />, toneClassName: 'bg-emerald-50' };
  }
  if (role === 'pipeline') {
    return { icon: <Network className="size-4 text-blue-700" />, toneClassName: 'bg-blue-50' };
  }
  return { icon: <PackageOpen className="size-4 text-slate-700" />, toneClassName: 'bg-slate-100' };
}

export const PipelineNode = memo(PipelineNodeComponent);
