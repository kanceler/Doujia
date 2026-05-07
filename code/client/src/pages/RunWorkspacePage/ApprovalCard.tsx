import { useState } from 'react';
import { useParams } from 'react-router-dom';
import type { MessageItem } from '@shared/api.interface';
import {
  approveDevflowCheckpoint,
  rejectDevflowCheckpoint,
} from '@/api/devflow-client';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';
import { Check, RotateCcw, X } from 'lucide-react';

interface ApprovalCardProps {
  message: MessageItem;
  onActionComplete: () => void;
}

export const ApprovalCard: React.FC<ApprovalCardProps> = ({ message, onActionComplete }) => {
  const { runId } = useParams<{ runId: string }>();
  const [comment, setComment] = useState<string>('');
  const [actionLoading, setActionLoading] = useState<string | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const approvalStatus: string | undefined = message.metadata?.approvalStatus;
  const isPending: boolean = approvalStatus === 'pending' || !approvalStatus;

  const handleApprove = async () => {
    if (!runId) return;
    setActionLoading('approve');
    setActionError(null);
    try {
      await approveDevflowCheckpoint(runId, message.id, { comment: comment || undefined });
      onActionComplete();
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Approve failed');
    } finally {
      setActionLoading(null);
    }
  };

  const handleReject = async () => {
    if (!runId) return;
    const reason = comment.trim();
    if (!reason) {
      setActionError('Reject reason is required');
      return;
    }
    setActionLoading('reject');
    setActionError(null);
    try {
      await rejectDevflowCheckpoint(runId, message.id, { reason, mode: 'replan' });
      onActionComplete();
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Reject failed');
    } finally {
      setActionLoading(null);
    }
  };

  const handleRewrite = async () => {
    if (!runId) return;
    const reason = comment.trim() || 'Request rewrite from workspace';
    setActionLoading('rewrite');
    setActionError(null);
    try {
      await rejectDevflowCheckpoint(runId, message.id, { reason, mode: 'rewrite' });
      onActionComplete();
    } catch (err) {
      setActionError(err instanceof Error ? err.message : 'Rewrite request failed');
    } finally {
      setActionLoading(null);
    }
  };

  if (!isPending) {
    return (
      <div className="text-sm">
        <p className="mb-3 leading-6 text-slate-700">{message.content}</p>
        <div className="flex items-center gap-2">
          <span className={`inline-flex items-center rounded-full px-2.5 py-1 text-xs font-medium ${
            approvalStatus === 'approved'
              ? 'bg-green-100 text-green-700'
              : approvalStatus === 'rejected'
                ? 'bg-red-100 text-red-700'
                : 'bg-muted text-muted-foreground'
          }`}>
            {approvalStatus ?? 'completed'}
          </span>
        </div>
      </div>
    );
  }

  return (
    <div className="space-y-4 rounded-[22px] border border-slate-200/80 bg-slate-50/80 p-4">
      <p className="text-sm leading-6 text-slate-700">{message.content}</p>
      <Textarea
        placeholder="Approval comment or reject reason"
        value={comment}
        onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) => setComment(e.target.value)}
        className="resize-none border-slate-200 bg-white text-sm shadow-none focus-visible:ring-2"
        rows={2}
      />
      {actionError && (
        <div className="text-xs text-destructive">
          {actionError}
        </div>
      )}
      <div className="flex items-center gap-2">
        <Button
          variant="default"
          size="sm"
          onClick={handleApprove}
          disabled={actionLoading !== null}
          className="gap-1 rounded-full px-4"
        >
          <Check className="size-3.5" />
          Approve
        </Button>
        <Button
          variant="destructive"
          size="sm"
          onClick={handleReject}
          disabled={actionLoading !== null}
          className="gap-1 rounded-full px-4"
        >
          <X className="size-3.5" />
          Reject
        </Button>
        <Button
          variant="outline"
          size="sm"
          onClick={handleRewrite}
          disabled={actionLoading !== null}
          className="gap-1 rounded-full px-4"
        >
          <RotateCcw className="size-3.5" />
          Rewrite
        </Button>
      </div>
    </div>
  );
};
