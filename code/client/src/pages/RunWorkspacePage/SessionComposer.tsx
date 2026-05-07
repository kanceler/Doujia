import { useState } from 'react';
import { CornerDownLeft, Send, Sparkles } from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Textarea } from '@/components/ui/textarea';

interface SessionComposerProps {
  disabled?: boolean;
  loading?: boolean;
  disabledReason?: string | null;
  onSend: (content: string) => Promise<void> | void;
}

export const SessionComposer: React.FC<SessionComposerProps> = ({
  disabled = false,
  loading = false,
  disabledReason,
  onSend,
}) => {
  const [value, setValue] = useState('');

  const trimmed = value.trim();
  const sendDisabled = disabled || loading || trimmed.length === 0;

  const handleSubmit = async () => {
    if (sendDisabled) return;
    const message = trimmed;
    setValue('');
    await onSend(message);
  };

  return (
    <div className="border-t border-transparent bg-transparent px-4 pb-4 pt-3 sm:px-6 sm:pb-6">
      <div className="mx-auto max-w-[1100px]">
        {disabledReason && (
          <div className="mb-3 rounded-[20px] border border-amber-200/80 bg-amber-50/90 px-4 py-3 text-xs text-amber-800 shadow-[0_10px_25px_rgba(245,158,11,0.08)]">
            {disabledReason}
          </div>
        )}

        <div className="overflow-hidden rounded-[28px] border border-white/80 bg-white/88 shadow-[0_20px_50px_rgba(15,23,42,0.08)] backdrop-blur">
          <div className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-100/90 px-4 py-3 sm:px-5">
            <div className="flex min-w-0 items-center gap-3">
              <div className="flex size-10 shrink-0 items-center justify-center rounded-[18px] bg-[radial-gradient(circle_at_top,#ffffff_0%,rgba(224,231,255,0.95)_34%,rgba(191,219,254,0.92)_70%,rgba(96,165,250,0.18)_100%)] text-primary shadow-[0_12px_30px_rgba(59,130,246,0.16)]">
                <Sparkles className="size-[18px]" />
              </div>
              <div className="min-w-0">
                <div className="text-sm font-semibold text-slate-950">继续和 Doujia 对话</div>
                <div className="text-xs text-slate-500">
                  {loading ? 'Doujia 正在实时回复...' : 'Enter 发送，Shift + Enter 换行'}
                </div>
              </div>
            </div>

            <div className="hidden items-center gap-2 rounded-full border border-slate-200/80 bg-slate-50/80 px-3 py-1.5 text-[11px] font-medium text-slate-500 sm:flex">
              <CornerDownLeft className="size-3.5" />
              真流式对话
            </div>
          </div>

          <div className="flex items-end gap-3 px-4 pb-4 pt-3 sm:px-5">
            <div className="flex-1 rounded-[22px] border border-slate-200/80 bg-slate-50/80 px-4 py-3 transition-[border-color,background-color,box-shadow] focus-within:border-primary/35 focus-within:bg-white focus-within:shadow-[0_14px_30px_rgba(59,130,246,0.08)]">
              <Textarea
                value={value}
                onChange={(event) => setValue(event.target.value)}
                onKeyDown={(event) => {
                  if (event.key === 'Enter' && !event.shiftKey) {
                    event.preventDefault();
                    void handleSubmit();
                  }
                }}
                disabled={disabled || loading}
                rows={3}
                placeholder="输入你的需求、修改意见或下一步想法..."
                className="min-h-[72px] max-h-[180px] resize-none border-0 bg-transparent px-0 py-0 text-[15px] leading-7 shadow-none focus-visible:ring-0 placeholder:text-slate-400"
              />
            </div>

            <Button
              type="button"
              size="icon"
              onClick={() => void handleSubmit()}
              disabled={sendDisabled}
              aria-label="Send Doujia message"
              className="size-12 rounded-[18px] border-transparent bg-[linear-gradient(135deg,#4f8cff_0%,#2563eb_100%)] text-white shadow-[0_18px_34px_rgba(37,99,235,0.28)] hover:shadow-[0_22px_36px_rgba(37,99,235,0.32)]"
            >
              <Send className="size-[18px]" />
            </Button>
          </div>
        </div>
      </div>
    </div>
  );
};
