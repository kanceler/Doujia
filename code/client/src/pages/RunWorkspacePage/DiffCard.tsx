import type { MessageItem } from '@shared/api.interface';

interface DiffCardProps {
  message: MessageItem;
}

export const DiffCard: React.FC<DiffCardProps> = ({ message }) => {
  const diffContent: string = message.metadata?.diffContent || message.content;

  return (
    <div className="space-y-3 rounded-[22px] border border-slate-200/80 bg-slate-50/80 p-4">
      <div className="flex items-center gap-2 text-xs font-semibold tracking-[0.02em] text-slate-500">
        <span className="inline-flex rounded-full bg-slate-200/70 px-2 py-1">代码变更</span>
      </div>
      <pre className="max-h-[220px] overflow-x-auto overflow-y-auto rounded-[18px] bg-slate-950 p-4 text-xs font-mono text-slate-100">
        <code>{diffContent}</code>
      </pre>
    </div>
  );
};
