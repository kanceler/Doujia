import { useState } from 'react';
import type { MessageItem } from '@shared/api.interface';
import { Button } from '@/components/ui/button';

interface RequirementSummaryCardProps {
  message: MessageItem;
  onActionComplete: () => void;
}

export const RequirementSummaryCard: React.FC<RequirementSummaryCardProps> = ({
  message,
  onActionComplete,
}) => {
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const summaryMeta = message.metadata?.requirementSummary;
  const confirmLabel = summaryMeta?.confirmActionLabel ?? '确认加入需求池';
  const reviseLabel = summaryMeta?.reviseActionLabel ?? '暂不采纳';

  const handleConfirm = async () => {
    if (!summaryMeta?.onConfirm) return;
    setLoading(true);
    setError(null);
    try {
      await summaryMeta.onConfirm();
      onActionComplete();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Failed to confirm requirement summary');
    } finally {
      setLoading(false);
    }
  };

  return (
    <div className="space-y-4 rounded-[22px] border border-primary/12 bg-[linear-gradient(180deg,rgba(239,246,255,0.92)_0%,rgba(255,255,255,0.96)_100%)] p-4 shadow-[0_10px_28px_rgba(59,130,246,0.08)]">
      <div className="flex items-center gap-2 text-xs font-semibold tracking-[0.02em] text-primary">
        <span className="inline-flex rounded-full bg-primary/10 px-2 py-1">需求摘要</span>
      </div>
      <div className="whitespace-pre-wrap text-sm leading-7 text-slate-700">
        {message.content}
      </div>
      {error && <div className="text-xs text-destructive">{error}</div>}
      <div className="flex flex-wrap gap-2">
        <Button
          size="sm"
          onClick={() => void handleConfirm()}
          disabled={loading || !summaryMeta?.onConfirm}
          className="rounded-full px-4"
        >
          {loading ? '确认中...' : confirmLabel}
        </Button>
        <Button size="sm" variant="outline" disabled className="rounded-full px-4">
          {reviseLabel}
        </Button>
      </div>
    </div>
  );
};

