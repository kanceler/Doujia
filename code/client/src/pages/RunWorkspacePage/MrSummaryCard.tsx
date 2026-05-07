import type { MessageItem } from '@shared/api.interface';
import { GitPullRequest } from 'lucide-react';

interface MrSummaryCardProps {
  message: MessageItem;
}

export const MrSummaryCard: React.FC<MrSummaryCardProps> = ({ message }) => {
  return (
    <div className="space-y-3 rounded-[22px] border border-slate-200/80 bg-slate-50/80 p-4">
      <div className="flex items-center gap-2 text-xs font-semibold tracking-[0.02em] text-slate-500">
        <GitPullRequest className="size-3.5" />
        <span>交付摘要</span>
      </div>
      <div className="rounded-[18px] bg-white/80 px-4 py-3 text-sm leading-7 text-slate-700">
        {message.content}
      </div>
    </div>
  );
};
