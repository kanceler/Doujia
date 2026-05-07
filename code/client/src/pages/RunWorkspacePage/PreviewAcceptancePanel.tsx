import { useEffect, useMemo, useRef, useState } from 'react';
import type {
  DevflowPreviewBrowserOpenResult,
  DevflowPreviewEditorOperation,
  DevflowPreviewOperationType,
  DevflowPreviewRepairPrepareResult,
  DevflowPreviewSelectedNode,
  DevflowPreviewStartResult,
  DevflowPreviewSubmitEditsResult,
} from '@shared/devflow-api';
import {
  callDevflowPreviewBrowserOpen,
  callDevflowPreviewConsole,
  callDevflowPreviewInspector,
  callDevflowPreviewRepairPrepare,
  callDevflowPreviewStart,
  callDevflowPreviewSubmitEdits,
} from '@/api/devflow-client';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { ScrollArea } from '@/components/ui/scroll-area';
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select';
import { Textarea } from '@/components/ui/textarea';
import {
  CheckCircle2,
  Code2,
  Edit3,
  ExternalLink,
  MousePointer2,
  Play,
  RefreshCw,
  Send,
  Wand2,
} from 'lucide-react';
import { PREVIEW_EVENTS, PREVIEW_HANDLER_NAMES, PREVIEW_OPERATION_TYPES } from './preview-contract';

interface PreviewAcceptancePanelProps {
  runId: string;
}

type PreviewLoadingAction =
  | null
  | 'preview_start'
  | 'preview_browser_open'
  | 'preview_inspector'
  | 'preview_console'
  | 'preview_submit_edits'
  | 'preview_repair_prepare';

const demoSelectedNode: DevflowPreviewSelectedNode = {
  selector: "[data-doujia-id='hero-title']",
  tag: 'h1',
  text: 'Hero title',
  attributes: {
    'data-doujia-id': 'hero-title',
    'data-doujia-file': 'src/pages/Home.tsx',
  },
};

export const PreviewAcceptancePanel: React.FC<PreviewAcceptancePanelProps> = ({ runId }) => {
  const iframeRef = useRef<HTMLIFrameElement>(null);
  const [startResult, setStartResult] = useState<DevflowPreviewStartResult | null>(null);
  const [browserPlan, setBrowserPlan] = useState<DevflowPreviewBrowserOpenResult | null>(null);
  const [inspectorScript, setInspectorScript] = useState('');
  const [consoleScript, setConsoleScript] = useState('');
  const [selectedNode, setSelectedNode] = useState<DevflowPreviewSelectedNode | null>(null);
  const [operationType, setOperationType] = useState<DevflowPreviewOperationType>('set_text');
  const [operationValue, setOperationValue] = useState('');
  const [operationProperty, setOperationProperty] = useState('');
  const [operationVariant, setOperationVariant] = useState('');
  const [submitResult, setSubmitResult] = useState<DevflowPreviewSubmitEditsResult | null>(null);
  const [repairInstruction, setRepairInstruction] = useState('');
  const [repairResult, setRepairResult] = useState<DevflowPreviewRepairPrepareResult | null>(null);
  const [loadingAction, setLoadingAction] = useState<PreviewLoadingAction>(null);
  const [error, setError] = useState<string | null>(null);
  const previewUrl = browserPlan?.preview_url || startResult?.preview_url || '';

  useEffect(() => {
    const handleSelection = (event: Event) => {
      const customEvent = event as CustomEvent<DevflowPreviewSelectedNode>;
      if (customEvent.detail?.selector) {
        setSelectedNode(customEvent.detail);
      }
    };
    window.addEventListener(PREVIEW_EVENTS.selection, handleSelection);
    return () => window.removeEventListener(PREVIEW_EVENTS.selection, handleSelection);
  }, []);

  const attachIframeSelectionListener = () => {
    const frameWindow = iframeRef.current?.contentWindow;
    if (!frameWindow) return;
    const handleSelection = (event: Event) => {
      const customEvent = event as CustomEvent<DevflowPreviewSelectedNode>;
      if (customEvent.detail?.selector) {
        setSelectedNode(customEvent.detail);
      }
    };
    iframeRef.current?.contentWindow?.addEventListener(PREVIEW_EVENTS.selection, handleSelection);
    return () => iframeRef.current?.contentWindow?.removeEventListener(PREVIEW_EVENTS.selection, handleSelection);
  };

  const selectedAttributes = selectedNode?.attributes ?? {};
  const currentOperation = useMemo<DevflowPreviewEditorOperation>(() => {
    const target = selectedNode?.selector || '';
    if (operationType === 'apply_variant') {
      return { type: operationType, target, variant: operationVariant.trim() };
    }
    if (operationType === 'delete_node') {
      return { type: operationType, target };
    }
    return {
      type: operationType,
      target,
      property: operationType === 'set_text' ? undefined : operationProperty.trim(),
      value: operationValue.trim(),
    };
  }, [operationProperty, operationType, operationValue, operationVariant, selectedNode?.selector]);

  const runAction = async <T,>(action: PreviewLoadingAction, task: () => Promise<T>): Promise<T | null> => {
    setLoadingAction(action);
    setError(null);
    try {
      return await task();
    } catch (err) {
      setError(err instanceof Error ? err.message : 'Preview handler failed');
      return null;
    } finally {
      setLoadingAction(null);
    }
  };

  const handleStart = async () => {
    const result = await runAction(PREVIEW_HANDLER_NAMES.start, () => callDevflowPreviewStart(runId));
    if (result) setStartResult(result);
  };

  const handleBrowserOpen = async () => {
    const sessionId = startResult?.session_id;
    if (!sessionId) {
      setError('preview_browser_open requires session_id from preview_start');
      return;
    }
    const result = await runAction(PREVIEW_HANDLER_NAMES.browserOpen, () =>
      callDevflowPreviewBrowserOpen(runId, { session_id: sessionId }),
    );
    if (result) setBrowserPlan(result);
  };

  const injectScript = (script: string) => {
    const frameDocument = iframeRef.current?.contentDocument;
    if (!frameDocument) {
      setError('Preview iframe is not ready for script injection');
      return;
    }
    const scriptElement = frameDocument.createElement('script');
    scriptElement.textContent = script;
    frameDocument.documentElement.appendChild(scriptElement);
    attachIframeSelectionListener();
  };

  const handleInspector = async () => {
    const result = await runAction(PREVIEW_HANDLER_NAMES.inspector, () => callDevflowPreviewInspector(runId));
    if (!result) return;
    setInspectorScript(result.script);
    injectScript(result.script);
    enableEditorMode();
  };

  const handleConsole = async () => {
    const result = await runAction(PREVIEW_HANDLER_NAMES.console, () => callDevflowPreviewConsole(runId));
    if (!result) return;
    setConsoleScript(result.script);
    injectScript(result.script);
  };

  const handleInjectBrowserPlan = () => {
    if (!browserPlan?.injection_script) {
      setError('preview_browser_open has not returned injection_script');
      return;
    }
    injectScript(browserPlan.injection_script);
  };

  const enableEditorMode = () => {
    iframeRef.current?.contentWindow?.dispatchEvent(
      new CustomEvent(PREVIEW_EVENTS.editorMode, {
        detail: { enabled: true },
      }),
    );
  };

  const handleSubmit = async () => {
    if (!startResult?.session_id) {
      setError('preview_submit_edits requires session_id from preview_start');
      return;
    }
    if (!selectedNode) {
      setError('preview_submit_edits requires selected_node from doujia:selection');
      return;
    }
    const result = await runAction(PREVIEW_HANDLER_NAMES.submitEdits, () =>
      callDevflowPreviewSubmitEdits(runId, {
        session_id: startResult.session_id,
        selected_node: selectedNode,
        operations: [currentOperation],
      }),
    );
    if (result) setSubmitResult(result);
  };

  const handleRepairPrepare = async () => {
    const instruction = repairInstruction.trim();
    if (!instruction) {
      setError('preview_repair_prepare requires repair_instruction');
      return;
    }
    const result = await runAction(PREVIEW_HANDLER_NAMES.repairPrepare, () =>
      callDevflowPreviewRepairPrepare(runId, { repair_instruction: instruction }),
    );
    if (result) setRepairResult(result);
  };

  const handleUseDemoSelection = () => {
    setSelectedNode(demoSelectedNode);
    setOperationValue('Better hero title');
    setOperationProperty(operationProperty || 'color');
  };

  const handleRefreshIframe = () => {
    const iframe = iframeRef.current;
    if (iframe?.src) iframe.src = iframe.src;
  };

  const hostFlow = [
    PREVIEW_HANDLER_NAMES.start,
    PREVIEW_HANDLER_NAMES.browserOpen,
    'injection_script',
    'doujia:editor-mode',
    PREVIEW_EVENTS.selection,
    PREVIEW_EVENTS.submitEdits,
    PREVIEW_HANDLER_NAMES.submitEdits,
    PREVIEW_HANDLER_NAMES.repairPrepare,
  ];

  const hostContractMarkers = ['set_text', 'set_style', 'set_layout', 'delete_node', 'apply_variant'];

  return (
    <section className="flex h-full min-h-0 flex-col bg-white">
      <div className="border-b border-slate-100 px-4 py-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <div className="text-sm font-semibold text-slate-950">Preview acceptance</div>
            <div className="mt-1 flex flex-wrap items-center gap-1.5 text-[11px] text-slate-500">
              {hostFlow.map((item, index) => (
                <span key={`${item}-${index}`} className="inline-flex items-center gap-1">
                  <code className="rounded-full border border-slate-200 bg-slate-50 px-2 py-0.5">{item}</code>
                  {index < hostFlow.length - 1 && <span className="text-slate-300">/</span>}
                </span>
              ))}
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <StatusPill active={Boolean(startResult)} label="session_id" />
            <StatusPill active={Boolean(previewUrl)} label="preview_url" />
            <StatusPill active={Boolean(browserPlan?.injection_script || inspectorScript || consoleScript)} label="script" />
          </div>
        </div>
      </div>

      {error && <div className="border-b border-red-100 bg-red-50 px-4 py-2 text-xs text-red-700">{error}</div>}

      <div className="grid min-h-0 flex-1 grid-cols-[minmax(520px,1fr)_360px] gap-0">
        <div className="flex min-h-0 flex-col border-r border-slate-100">
          <div className="flex flex-wrap items-center gap-2 border-b border-slate-100 px-3 py-2">
            <ActionButton loading={loadingAction === PREVIEW_HANDLER_NAMES.start} onClick={() => void handleStart()} icon={<Play className="size-3.5" />}>
              preview_start
            </ActionButton>
            <ActionButton loading={loadingAction === PREVIEW_HANDLER_NAMES.browserOpen} onClick={() => void handleBrowserOpen()} icon={<ExternalLink className="size-3.5" />}>
              preview_browser_open
            </ActionButton>
            <ActionButton loading={false} onClick={handleInjectBrowserPlan} icon={<Code2 className="size-3.5" />}>
              injection_script
            </ActionButton>
            <ActionButton loading={loadingAction === PREVIEW_HANDLER_NAMES.inspector} onClick={() => void handleInspector()} icon={<MousePointer2 className="size-3.5" />}>
              preview_inspector
            </ActionButton>
            <ActionButton loading={loadingAction === PREVIEW_HANDLER_NAMES.console} onClick={() => void handleConsole()} icon={<Wand2 className="size-3.5" />}>
              preview_console
            </ActionButton>
            <Button variant="outline" size="sm" className="ml-auto rounded-full" onClick={handleRefreshIframe}>
              <RefreshCw className="size-3.5" />
              refresh iframe
            </Button>
          </div>

          <div className="relative min-h-0 flex-1 bg-slate-50">
            {previewUrl ? (
              <iframe ref={iframeRef} title="preview_url" src={previewUrl} className="h-full w-full border-0 bg-white" />
            ) : (
              <PreviewEmptyState onUseDemoSelection={handleUseDemoSelection} />
            )}
            {selectedNode && (
              <div className="pointer-events-none absolute left-[13%] top-[22%] w-[46%] rounded-md border-2 border-blue-500 bg-blue-500/5 shadow-[0_0_0_9999px_rgba(15,23,42,0.02)]">
                <div className="-mt-7 inline-flex max-w-full items-center gap-1 rounded-full bg-blue-600 px-2 py-1 text-[11px] font-medium text-white shadow-sm">
                  <span className="truncate">{selectedAttributes['data-doujia-id'] || selectedNode.selector}</span>
                  <span className="text-blue-100">/</span>
                  <span className="truncate">{selectedAttributes['data-doujia-file'] || 'no data-doujia-file'}</span>
                </div>
              </div>
            )}
          </div>
        </div>

        <aside className="flex min-h-0 flex-col">
          <ScrollArea className="min-h-0 flex-1">
            <div className="space-y-4 p-4">
              <section className="rounded-lg border border-slate-200 bg-white p-3">
                <div className="flex items-center justify-between gap-2">
                  <div className="text-sm font-semibold text-slate-900">selected_node</div>
                  <Button type="button" variant="outline" size="sm" className="rounded-full" onClick={handleUseDemoSelection}>
                    demo selection
                  </Button>
                </div>
                <KeyValue label="selector" value={selectedNode?.selector} />
                <KeyValue label="tag" value={selectedNode?.tag} />
                <KeyValue label="text" value={selectedNode?.text} />
                <KeyValue label="data-doujia-id" value={selectedAttributes['data-doujia-id']} />
                <KeyValue label="data-doujia-file" value={selectedAttributes['data-doujia-file']} />
              </section>

              <section className="rounded-lg border border-slate-200 bg-white p-3">
                <div className="mb-3 flex items-center gap-2 text-sm font-semibold text-slate-900">
                  <Edit3 className="size-4 text-blue-600" />
                  operations
                </div>
                <div className="mb-3 flex flex-wrap gap-1">
                  {hostContractMarkers.map((marker) => (
                    <code key={marker} className="rounded-full border border-slate-200 bg-slate-50 px-2 py-0.5 text-[11px] text-slate-600">
                      {marker}
                    </code>
                  ))}
                </div>
                <div className="space-y-3">
                  <div>
                    <label className="mb-1 block text-xs font-medium text-slate-600">type</label>
                    <Select value={operationType} onValueChange={(value) => setOperationType(value as DevflowPreviewOperationType)}>
                      <SelectTrigger className="w-full">
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        {PREVIEW_OPERATION_TYPES.map((type) => (
                          <SelectItem key={type} value={type}>
                            {type}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <div>
                    <label className="mb-1 block text-xs font-medium text-slate-600">target</label>
                    <Input value={selectedNode?.selector || ''} readOnly placeholder="from doujia:selection" />
                  </div>
                  {operationType !== 'set_text' && operationType !== 'delete_node' && operationType !== 'apply_variant' && (
                    <div>
                      <label className="mb-1 block text-xs font-medium text-slate-600">property</label>
                      <Input value={operationProperty} onChange={(event) => setOperationProperty(event.target.value)} placeholder="color / padding / border-radius" />
                    </div>
                  )}
                  {operationType === 'apply_variant' ? (
                    <div>
                      <label className="mb-1 block text-xs font-medium text-slate-600">variant</label>
                      <Input value={operationVariant} onChange={(event) => setOperationVariant(event.target.value)} placeholder="primary" />
                    </div>
                  ) : operationType !== 'delete_node' ? (
                    <div>
                      <label className="mb-1 block text-xs font-medium text-slate-600">value</label>
                      <Textarea value={operationValue} onChange={(event) => setOperationValue(event.target.value)} placeholder="text or CSS value" />
                    </div>
                  ) : (
                    <div className="rounded-md border border-amber-200 bg-amber-50 px-3 py-2 text-xs text-amber-800">
                      delete_node enters structural source repair on the backend.
                    </div>
                  )}
                  <Button className="w-full" onClick={() => void handleSubmit()} disabled={!selectedNode || loadingAction === PREVIEW_HANDLER_NAMES.submitEdits}>
                    <Send className="size-4" />
                    {loadingAction === PREVIEW_HANDLER_NAMES.submitEdits ? 'submitting...' : 'preview_submit_edits'}
                  </Button>
                </div>
              </section>

              <section className="rounded-lg border border-slate-200 bg-white p-3">
                <div className="mb-2 text-sm font-semibold text-slate-900">agent repair fallback</div>
                <div className="text-xs leading-5 text-slate-500">
                  When local source mapping cannot commit the selected operation, call preview_repair_prepare with only repair_instruction.
                </div>
                <Textarea
                  className="mt-3"
                  value={repairInstruction}
                  onChange={(event) => setRepairInstruction(event.target.value)}
                  placeholder="repair_instruction from preview_submit_edits agent mode"
                />
                <Button
                  type="button"
                  variant="outline"
                  className="mt-3 w-full"
                  onClick={() => void handleRepairPrepare()}
                  disabled={loadingAction === PREVIEW_HANDLER_NAMES.repairPrepare}
                >
                  <Wand2 className="size-4" />
                  {loadingAction === PREVIEW_HANDLER_NAMES.repairPrepare ? 'preparing...' : 'preview_repair_prepare'}
                </Button>
              </section>

              <section className="rounded-lg border border-slate-200 bg-white p-3">
                <div className="mb-2 text-sm font-semibold text-slate-900">handler data</div>
                <ResultBlock title="preview_start" value={startResult} />
                <ResultBlock title="preview_browser_open" value={browserPlan} />
                <ResultBlock title="preview_submit_edits" value={submitResult} />
                <ResultBlock title="preview_repair_prepare" value={repairResult} />
              </section>
            </div>
          </ScrollArea>
        </aside>
      </div>
    </section>
  );
};

const StatusPill: React.FC<{ active: boolean; label: string }> = ({ active, label }) => (
  <Badge variant="outline" className={active ? 'border-emerald-200 bg-emerald-50 text-emerald-700' : 'border-slate-200 bg-slate-50 text-slate-500'}>
    {active && <CheckCircle2 className="mr-1 size-3" />}
    {label}
  </Badge>
);

const ActionButton: React.FC<{
  children: React.ReactNode;
  icon: React.ReactNode;
  loading: boolean;
  onClick: () => void;
}> = ({ children, icon, loading, onClick }) => (
  <Button type="button" variant="outline" size="sm" className="rounded-full" onClick={onClick} disabled={loading}>
    {icon}
    {loading ? 'calling...' : children}
  </Button>
);

const KeyValue: React.FC<{ label: string; value?: string }> = ({ label, value }) => (
  <div className="mt-2 grid grid-cols-[112px_minmax(0,1fr)] gap-2 text-xs">
    <div className="font-medium text-slate-500">{label}</div>
    <code className="min-w-0 break-all rounded bg-slate-50 px-2 py-1 text-slate-700">
      {value?.trim() || '-'}
    </code>
  </div>
);

const ResultBlock: React.FC<{ title: string; value: unknown }> = ({ title, value }) => (
  <div className="mt-2">
    <div className="text-xs font-medium text-slate-500">{title}</div>
    <pre className="mt-1 max-h-36 overflow-auto rounded-md border border-slate-100 bg-slate-50 p-2 text-[11px] leading-5 text-slate-700">
      {value ? JSON.stringify(value, null, 2) : 'not called'}
    </pre>
  </div>
);

const PreviewEmptyState: React.FC<{ onUseDemoSelection: () => void }> = ({ onUseDemoSelection }) => (
  <div className="flex h-full items-center justify-center p-6">
    <div className="max-w-md rounded-lg border border-dashed border-slate-200 bg-white p-5 text-center shadow-sm">
      <div className="text-sm font-semibold text-slate-900">Waiting for preview_url</div>
      <div className="mt-2 text-xs leading-5 text-slate-500">
        Call preview_start and preview_browser_open first. The returned preview_url will render here as the host iframe.
      </div>
      <Button type="button" variant="outline" size="sm" className="mt-4 rounded-full" onClick={onUseDemoSelection}>
        simulate doujia:selection
      </Button>
    </div>
  </div>
);
