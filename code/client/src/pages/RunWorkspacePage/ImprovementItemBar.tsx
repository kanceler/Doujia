import type { DevflowImprovementItemView } from '@shared/devflow-api';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Sparkles } from 'lucide-react';

interface ImprovementItemBarProps {
  items: DevflowImprovementItemView[];
  awaitingAcceptance?: boolean;
  onOpenContinueDialog?: () => void;
}

export const ImprovementItemBar: React.FC<ImprovementItemBarProps> = ({
  items,
  awaitingAcceptance = false,
  onOpenContinueDialog,
}) => {
  const visibleItems = items.filter((item) => item.status === 'open');

  if (visibleItems.length === 0 && !awaitingAcceptance) {
    return null;
  }

  return (
    <div className="shrink-0 px-4 pt-3 sm:px-6">
      <div className="mx-auto max-w-[1100px] overflow-hidden rounded-[24px] border border-white/80 bg-white/72 shadow-[0_12px_36px_rgba(15,23,42,0.05)] backdrop-blur">
        <div className="flex flex-wrap items-center justify-between gap-3 px-4 py-3 sm:px-5">
          <div className="flex min-w-0 items-center gap-3">
            <div className="flex size-9 shrink-0 items-center justify-center rounded-[16px] bg-primary/10 text-primary">
              <Sparkles className="size-4" />
            </div>

            <div className="min-w-0">
              <div className="text-sm font-semibold text-slate-950">需求补充池</div>
              <div className="text-xs text-slate-500">
                {visibleItems.length > 0
                  ? `当前有 ${visibleItems.length} 条待进入下一轮的补充项`
                  : '当前没有待推进的补充项'}
              </div>
            </div>
          </div>

          {awaitingAcceptance && onOpenContinueDialog && (
            <Button
              size="sm"
              variant="outline"
              onClick={onOpenContinueDialog}
              className="rounded-full border-slate-200 bg-white px-4"
            >
              继续补充
            </Button>
          )}
        </div>

        {visibleItems.length > 0 && (
          <ScrollArea className="w-full whitespace-nowrap">
            <div className="flex gap-3 px-4 pb-4 sm:px-5">
              {visibleItems.map((item) => (
                <div
                  key={item.item_id}
                  className="min-w-[220px] max-w-[320px] rounded-[20px] border border-slate-200/80 bg-slate-50/85 px-4 py-3 shadow-[0_6px_18px_rgba(15,23,42,0.03)]"
                  title={item.detail}
                >
                  <div className="flex items-center justify-between gap-2">
                    <div className="truncate text-sm font-medium text-slate-900">{item.title}</div>
                    <Badge variant="secondary" className="rounded-full text-[10px]">
                      {item.status}
                    </Badge>
                  </div>
                  <div className="mt-2 line-clamp-2 whitespace-normal text-xs leading-5 text-slate-500">
                    {item.detail || '暂无补充说明'}
                  </div>
                </div>
              ))}
            </div>
          </ScrollArea>
        )}
      </div>
    </div>
  );
};
