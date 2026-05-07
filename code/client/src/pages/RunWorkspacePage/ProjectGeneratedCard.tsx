import type { MessageItem } from '@shared/api.interface';
import { Button } from '@/components/ui/button';
import { CheckCircle2, ExternalLink, Layers3, Sparkles } from 'lucide-react';

interface ProjectGeneratedCardProps {
  message: MessageItem;
  onOpen: () => void;
}

export const ProjectGeneratedCard: React.FC<ProjectGeneratedCardProps> = ({ message, onOpen }) => {
  const meta = message.metadata?.projectGenerated;
  const title = meta?.title || '项目已经生成';
  const description =
    meta?.description ||
    message.content ||
    '代码、测试和合并结果已经完成，可以进入预览验收查看生成效果。';
  const actionLabel = meta?.actionLabel || '点击查看';

  return (
    <button
      type="button"
      onClick={onOpen}
      className="group block w-full rounded-[24px] border border-emerald-200/80 bg-[linear-gradient(180deg,rgba(240,253,250,0.96)_0%,rgba(255,255,255,0.98)_100%)] p-0 text-left shadow-[0_18px_46px_rgba(16,185,129,0.12)] transition hover:-translate-y-0.5 hover:border-emerald-300 hover:shadow-[0_22px_58px_rgba(16,185,129,0.18)] focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-emerald-400"
    >
      <div className="space-y-4 p-4">
        <div className="flex items-start gap-3">
          <div className="flex size-11 shrink-0 items-center justify-center rounded-[18px] bg-emerald-500 text-white shadow-[0_12px_26px_rgba(16,185,129,0.26)]">
            <CheckCircle2 className="size-5" />
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <div className="text-[15px] font-semibold text-slate-950">{title}</div>
              <span className="inline-flex items-center gap-1 rounded-full border border-emerald-200 bg-emerald-50 px-2 py-0.5 text-[10px] font-medium text-emerald-700">
                <Sparkles className="size-3" />
                已完成
              </span>
            </div>
            <p className="mt-1 text-sm leading-6 text-slate-600">{description}</p>
          </div>
        </div>

        <div className="grid grid-cols-[1fr_auto] items-center gap-3 rounded-[18px] border border-emerald-100/90 bg-white/82 px-3 py-3">
          <div className="flex min-w-0 items-center gap-2">
            <span className="flex size-8 shrink-0 items-center justify-center rounded-full bg-sky-50 text-sky-600">
              <Layers3 className="size-4" />
            </span>
            <div className="min-w-0">
              <div className="truncate text-xs font-medium text-slate-700">生成结果已准备好</div>
              <div className="truncate text-[11px] text-slate-400">Final result / delivery preview</div>
            </div>
          </div>
          <Button
            type="button"
            size="sm"
            className="rounded-full bg-slate-950 px-3 text-white hover:bg-slate-800"
            onClick={(event) => {
              event.stopPropagation();
              onOpen();
            }}
          >
            {actionLabel}
            <ExternalLink className="size-3.5" />
          </Button>
        </div>
      </div>
    </button>
  );
};
