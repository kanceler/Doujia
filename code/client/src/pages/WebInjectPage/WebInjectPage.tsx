import { useCallback, useEffect, useRef, useState } from 'react';
import { useParams } from 'react-router-dom';
import { capabilityClient } from '@lark-apaas/client-toolkit';
import { logger } from '@lark-apaas/client-toolkit/logger';
import {
  MousePointer2,
  Send,
  FileDiff,
  Loader2,
  CheckCircle2,
  AlertCircle,
  Sparkles,
} from 'lucide-react';
import { Button } from '@/components/ui/button';
import { Badge } from '@/components/ui/badge';
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from '@/components/ui/dialog';
import { Textarea } from '@/components/ui/textarea';
import { Alert, AlertTitle, AlertDescription } from '@/components/ui/alert';
import type {
  ModifyResponse,
  SubmitMrResponse,
  PreviewResponse,
} from '@shared/api.interface';
import type { MrSummaryGeneratorTwoOutput } from '@shared/plugin-types';
import * as webInjectApi from '@/api/web-inject';
import { UniversalLink } from '@lark-apaas/client-toolkit/components/UniversalLink';

type InjectStatus = 'idle' | 'selecting' | 'modifying' | 'submitting' | 'done' | 'error';

const WebInjectPage: React.FC = () => {
  const { runId } = useParams<{ runId: string }>();
  const [previewHtml, setPreviewHtml] = useState<string>('');
  const [status, setStatus] = useState<InjectStatus>('idle');
  const [selectedElement, setSelectedElement] = useState<HTMLElement | null>(null);
  const [elementSelector, setElementSelector] = useState<string>('');
  const [elementContent, setElementContent] = useState<string>('');
  const [modifyInstruction, setModifyInstruction] = useState<string>('');
  const [dialogOpen, setDialogOpen] = useState<boolean>(false);
  const [modifyResult, setModifyResult] = useState<ModifyResponse | null>(null);
  const [mrSummary, setMrSummary] = useState<string>('');
  const [mrSubmitting, setMrSubmitting] = useState<boolean>(false);
  const [mrResult, setMrResult] = useState<SubmitMrResponse | null>(null);
  const [errorMsg, setErrorMsg] = useState<string>('');
  const lastInstructionRef = useRef<string>('');
  const previewRef = useRef<HTMLDivElement>(null);
  const highlightRef = useRef<HTMLDivElement>(null);

  // Load preview
  useEffect(() => {
    if (!runId) return;
    let cancelled = false;
    webInjectApi
      .getPreview(runId)
      .then((res: PreviewResponse) => {
        if (!cancelled) setPreviewHtml(res.html);
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          logger.error('Failed to load preview:', err);
          setErrorMsg('加载预览失败，请重试');
          setStatus('error');
        }
      });
    return () => {
      cancelled = true;
    };
  }, [runId]);

  // Element selection handler
  const handlePreviewClick = useCallback(
    (e: React.MouseEvent<HTMLDivElement>) => {
      if (status !== 'selecting') return;
      const target = e.target as HTMLElement;
      if (target === previewRef.current || target === highlightRef.current) return;

      // Remove previous highlight
      const prev = previewRef.current?.querySelector('[data-inject-highlight]');
      if (prev) prev.removeAttribute('data-inject-highlight');

      // Add highlight to clicked element
      target.setAttribute('data-inject-highlight', 'true');
      setSelectedElement(target);
      setElementContent(target.textContent || '');

      // Generate simple selector
      const tag = target.tagName.toLowerCase();
      const id = target.id ? `#${target.id}` : '';
      const cls = target.className && typeof target.className === 'string'
        ? `.${target.className.trim().split(/\s+/).join('.')}`
        : '';
      setElementSelector(`${tag}${id}${cls}`);
      setDialogOpen(true);
      setStatus('modifying');
    },
    [status],
  );

  // Update highlight overlay position
  useEffect(() => {
    if (!selectedElement || !highlightRef.current || !previewRef.current) return;
    const overlay = highlightRef.current;
    const previewRect = previewRef.current.getBoundingClientRect();
    const elRect = selectedElement.getBoundingClientRect();

    overlay.style.left = `${elRect.left - previewRect.left}px`;
    overlay.style.top = `${elRect.top - previewRect.top}px`;
    overlay.style.width = `${elRect.width}px`;
    overlay.style.height = `${elRect.height}px`;
    overlay.style.display = 'block';
  }, [selectedElement]);

  // Submit modification
  const handleSubmitModify = useCallback(async () => {
    if (!runId || !elementSelector || !modifyInstruction) return;
    setStatus('modifying');
    setErrorMsg('');
    try {
      const res = await webInjectApi.submitModification(runId, {
        elementSelector,
        elementContent,
        modifyInstruction,
      });
      setModifyResult(res);
      setPreviewHtml(res.modifiedHtml);
      lastInstructionRef.current = modifyInstruction;
      setStatus('done');
      setDialogOpen(false);
      setSelectedElement(null);
      setModifyInstruction('');
    } catch (err: unknown) {
      logger.error('Modify failed:', err);
      setErrorMsg('修改提交失败，请重试');
      setStatus('error');
    }
  }, [runId, elementSelector, elementContent, modifyInstruction]);

  // Generate MR summary via plugin
  const handleGenerateSummary = useCallback(async () => {
    if (!modifyResult?.diff) return;
    setMrSummary('');
    setStatus('submitting');
    try {
      const stream = capabilityClient
        .load('mr_summary_generator_2')
        .callStream<MrSummaryGeneratorTwoOutput>('textGenerate', {
          diff: modifyResult.diff,
          changeDesc: lastInstructionRef.current || '网页元素修改',
        });

      for await (const chunk of stream) {
        const content = chunk?.content ?? '';
        setMrSummary((prev: string) => prev + content);
      }
    } catch (err: unknown) {
      logger.error('MR summary generation failed:', err);
      setErrorMsg('MR 摘要生成失败');
      setStatus('error');
    }
  }, [modifyResult, modifyInstruction]);

  // Submit MR
  const handleSubmitMr = useCallback(async () => {
    if (!runId || !modifyResult) return;
    setMrSubmitting(true);
    try {
      const res = await webInjectApi.submitMr(runId, {
        mrTitle: `WebInject: ${lastInstructionRef.current.slice(0, 50)}`,
        mrDescription: mrSummary || lastInstructionRef.current,
        diff: modifyResult.diff,
      });
      setMrResult(res);
      setMrSubmitting(false);
    } catch (err: unknown) {
      logger.error('MR submit failed:', err);
      setErrorMsg('MR 提交失败');
      setMrSubmitting(false);
    }
  }, [runId, modifyResult, mrSummary, modifyInstruction]);

  // Reset selection
  const handleCancelSelect = useCallback(() => {
    setStatus('idle');
    setSelectedElement(null);
    const prev = previewRef.current?.querySelector('[data-inject-highlight]');
    if (prev) prev.removeAttribute('data-inject-highlight');
    if (highlightRef.current) highlightRef.current.style.display = 'none';
  }, []);

  return (
    <div className="flex flex-col h-full bg-background">
      {/* Floating Toolbar */}
      <div className="flex items-center gap-2 px-4 py-2 border-b border-border bg-card">
        <Badge variant={status === 'selecting' ? 'default' : 'secondary'}>
          {status === 'selecting' ? '选择中' : status === 'modifying' ? '修改中' : status === 'done' ? '已完成' : status === 'error' ? '出错' : '就绪'}
        </Badge>
        <div className="flex-1" />
        {status !== 'selecting' && (
          <Button
            variant="outline"
            size="sm"
            onClick={() => setStatus('selecting')}
            className="gap-1"
          >
            <MousePointer2 className="size-4" />
            选择元素
          </Button>
        )}
        {status === 'selecting' && (
          <Button variant="secondary" size="sm" onClick={handleCancelSelect}>
            取消选择
          </Button>
        )}
        {status === 'done' && modifyResult && (
          <>
            <Button
              variant="outline"
              size="sm"
              onClick={handleGenerateSummary}
              className="gap-1"
            >
              <Sparkles className="size-4" />
              生成 MR 摘要
            </Button>
            <Button
              variant="default"
              size="sm"
              onClick={handleSubmitMr}
              disabled={mrSubmitting}
              className="gap-1"
            >
              {mrSubmitting ? (
                <Loader2 className="size-4 animate-spin" />
              ) : (
                <Send className="size-4" />
              )}
              提交 MR
            </Button>
          </>
        )}
      </div>

      {/* Main Content Area */}
      <div className="flex flex-1 overflow-hidden">
        {/* Preview Area */}
        <div className="flex-1 relative overflow-auto bg-muted/30">
          <div
            ref={previewRef}
            className={`relative min-h-full ${status === 'selecting' ? 'cursor-crosshair' : ''}`}
            onClick={handlePreviewClick}
          >
            <div
              dangerouslySetInnerHTML={{ __html: previewHtml }}
              className="bg-card mx-auto max-w-5xl min-h-full shadow-sm"
            />
            {/* Selection Highlight Overlay */}
            <div
              ref={highlightRef}
              className="absolute pointer-events-none hidden border-2 border-primary rounded-sm"
              style={{ boxShadow: '0 0 0 2px hsl(212 92% 48% / 0.2)' }}
            />
          </div>

          {/* Selecting Hint */}
          {status === 'selecting' && (
            <div className="absolute top-4 left-1/2 -translate-x-1/2 z-10">
              <Badge className="gap-1 px-3 py-1.5 shadow-md">
                <MousePointer2 className="size-3" />
                点击页面元素进行选择
              </Badge>
            </div>
          )}
        </div>

        {/* Right Panel - Status & MR Summary */}
        {(status === 'done' || status === 'submitting' || mrResult || errorMsg) && (
          <div className="w-80 border-l border-border bg-card overflow-auto p-4 flex flex-col gap-3">
            {errorMsg && (
              <Alert variant="destructive">
                <AlertCircle className="size-4" />
                <AlertTitle>错误</AlertTitle>
                <AlertDescription>{errorMsg}</AlertDescription>
              </Alert>
            )}

            {status === 'done' && modifyResult && (
              <Alert variant="success">
                <CheckCircle2 className="size-4" />
                <AlertTitle>修改成功</AlertTitle>
                <AlertDescription className="text-xs font-mono break-all">
                  {modifyResult.diff.slice(0, 200)}
                  {modifyResult.diff.length > 200 ? '...' : ''}
                </AlertDescription>
              </Alert>
            )}

            {status === 'submitting' && (
              <Alert>
                <Loader2 className="size-4 animate-spin" />
                <AlertTitle>正在生成 MR 摘要...</AlertTitle>
                <AlertDescription>
                  {mrSummary ? (
                    <div className="mt-2 text-sm whitespace-pre-wrap font-mono">
                      {mrSummary}
                    </div>
                  ) : (
                    'AI 正在分析代码变更'
                  )}
                </AlertDescription>
              </Alert>
            )}

            {mrSummary && status !== 'submitting' && (
              <div className="rounded-md border border-border p-3">
                <h4 className="text-sm font-semibold mb-2 flex items-center gap-1">
                  <Sparkles className="size-3 text-primary" />
                  MR 摘要
                </h4>
                <div className="text-sm whitespace-pre-wrap font-mono text-muted-foreground">
                  {mrSummary}
                </div>
              </div>
            )}

            {mrResult && (
              <Alert variant="success">
                <CheckCircle2 className="size-4" />
                <AlertTitle>MR 已提交</AlertTitle>
                <AlertDescription>
                  {mrResult.mrUrl ? (
                    <UniversalLink
                      to={mrResult.mrUrl}
                      target="_blank"
                      rel="noopener noreferrer"
                      className="text-primary underline text-xs break-all"
                    >
                      查看 MR →
                    </UniversalLink>
                  ) : (
                    '合并请求已成功创建'
                  )}
                </AlertDescription>
              </Alert>
            )}
          </div>
        )}
      </div>

      {/* Modification Dialog */}
      <Dialog open={dialogOpen} onOpenChange={(open: boolean) => {
        if (!open) {
          setDialogOpen(false);
          handleCancelSelect();
        }
      }}>
        <DialogContent className="max-w-xl">
          <DialogHeader>
            <DialogTitle>修改元素</DialogTitle>
            <DialogDescription>
              已选择元素：<code className="text-xs bg-muted px-1.5 py-0.5 rounded">{elementSelector}</code>
            </DialogDescription>
          </DialogHeader>

          <div className="space-y-3 py-2">
            {/* Element Content Preview */}
            <div>
              <label className="text-sm font-medium mb-1 block">当前内容</label>
              <div className="rounded-md border border-border bg-muted/50 p-3 text-sm font-mono max-h-32 overflow-auto break-words">
                {elementContent || '(空)'}
              </div>
            </div>

            {/* Modification Instruction */}
            <div>
              <label className="text-sm font-medium mb-1 block">修改指令</label>
              <Textarea
                value={modifyInstruction}
                onChange={(e: React.ChangeEvent<HTMLTextAreaElement>) => setModifyInstruction(e.target.value)}
                placeholder="描述你想要的修改，例如：将按钮颜色改为蓝色，文字改为'提交订单'"
                className="min-h-[100px]"
              />
            </div>
          </div>

          <DialogFooter>
            <Button
              variant="outline"
              onClick={() => {
                setDialogOpen(false);
                handleCancelSelect();
              }}
            >
              取消
            </Button>
            <Button
              onClick={handleSubmitModify}
              disabled={!modifyInstruction.trim()}
              className="gap-1"
            >
              <FileDiff className="size-4" />
              提交修改
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  );
};

export default WebInjectPage;
