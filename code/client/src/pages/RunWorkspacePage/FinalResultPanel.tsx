import { useEffect, useState } from 'react';
import type { DevflowFinalResultOpenResult } from '@shared/devflow-api';
import { callDevflowFinalResultOpen } from '@/api/devflow-client';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import { AlertTriangle, ExternalLink, Loader2, Play, RefreshCw } from 'lucide-react';

interface FinalResultPanelProps {
  runId: string;
  bagId?: string;
  containerId?: string;
  previewUrl?: string;
}

export const FinalResultPanel: React.FC<FinalResultPanelProps> = ({
  runId,
  bagId,
  containerId,
  previewUrl,
}) => {
  const [result, setResult] = useState<DevflowFinalResultOpenResult | null>(
    previewUrl
      ? {
          status: 'ready',
          preview_url: previewUrl,
          container_id: containerId,
          bag_id: bagId,
        }
      : null,
  );
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const iframeUrl = result?.preview_url || previewUrl || '';

  const openFinalResult = async () => {
    if (!runId) return;
    if (!bagId && !containerId && !previewUrl) {
      setError('缺少最终结果 bag_id / container_id / preview_url，无法启动结果容器。');
      return;
    }
    setLoading(true);
    setError(null);
    try {
      const opened = await callDevflowFinalResultOpen(runId, {
        bag_id: bagId,
        container_id: containerId,
        preview_url: previewUrl,
      });
      setResult(opened);
    } catch (err) {
      setError(err instanceof Error ? err.message : '最终结果启动失败');
    } finally {
      setLoading(false);
    }
  };

  useEffect(() => {
    if (previewUrl) return;
    if (!bagId && !containerId) return;
    void openFinalResult();
    // open when a new final-result tab receives bag/container identity
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [bagId, containerId, runId]);

  return (
    <section className="flex h-full min-h-0 flex-col bg-white">
      <div className="flex flex-wrap items-center justify-between gap-3 border-b border-slate-100 px-4 py-3">
        <div>
          <div className="text-sm font-semibold text-slate-950">最终结果</div>
          <div className="mt-1 flex flex-wrap items-center gap-1.5 text-[11px] text-slate-500">
            <Badge variant="outline" className="border-slate-200 bg-slate-50 text-slate-600">
              bag_id: {bagId || '-'}
            </Badge>
            <Badge variant="outline" className="border-slate-200 bg-slate-50 text-slate-600">
              container_id: {result?.container_id || containerId || '-'}
            </Badge>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {iframeUrl && (
            <Button type="button" variant="outline" size="sm" className="rounded-full" asChild>
              <a href={iframeUrl} target="_blank" rel="noreferrer">
                <ExternalLink className="size-3.5" />
                新窗口打开
              </a>
            </Button>
          )}
          <Button
            type="button"
            variant="outline"
            size="sm"
            className="rounded-full"
            onClick={() => void openFinalResult()}
            disabled={loading}
          >
            {loading ? <Loader2 className="size-3.5 animate-spin" /> : iframeUrl ? <RefreshCw className="size-3.5" /> : <Play className="size-3.5" />}
            {iframeUrl ? '重新启动' : '启动最终结果'}
          </Button>
        </div>
      </div>

      {error && (
        <div className="border-b border-red-100 bg-red-50 px-4 py-3">
          <Alert variant="destructive" className="border-red-200 bg-white">
            <AlertTriangle className="size-4" />
            <AlertTitle>最终结果不可用</AlertTitle>
            <AlertDescription>{error}</AlertDescription>
          </Alert>
        </div>
      )}

      <div className="min-h-0 flex-1 bg-slate-50">
        {iframeUrl ? (
          <iframe
            key={iframeUrl}
            title="最终结果预览"
            src={iframeUrl}
            className="h-full w-full border-0 bg-white"
          />
        ) : (
          <div className="flex h-full items-center justify-center p-6">
            <div className="max-w-md rounded-lg border border-dashed border-slate-200 bg-white p-5 text-center shadow-sm">
              <div className="text-sm font-semibold text-slate-900">等待最终结果容器</div>
              <div className="mt-2 text-xs leading-5 text-slate-500">
                完成卡片会把 bag_id 或 container_id 带到这里。点击启动后，后端会根据 bag 找到容器并返回 preview_url。
              </div>
              <Button
                type="button"
                variant="outline"
                size="sm"
                className="mt-4 rounded-full"
                onClick={() => void openFinalResult()}
                disabled={loading}
              >
                {loading ? <Loader2 className="size-3.5 animate-spin" /> : <Play className="size-3.5" />}
                启动最终结果
              </Button>
            </div>
          </div>
        )}
      </div>
    </section>
  );
};
