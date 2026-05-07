import { readFileSync } from 'node:fs';
import { join } from 'node:path';

const root = join(__dirname, '..', '..');

describe('preview acceptance frontend wiring', () => {
  it('exposes frontend clients for the Doujia preview handlers', () => {
    const clientSource = readFileSync(join(root, 'client/src/api/devflow-client.ts'), 'utf8');
    const typeSource = readFileSync(join(root, 'shared/devflow-api.ts'), 'utf8');

    expect(clientSource).toContain('callDevflowPreviewStart');
    expect(clientSource).toContain('callDevflowPreviewBrowserOpen');
    expect(clientSource).toContain('callDevflowPreviewInspector');
    expect(clientSource).toContain('callDevflowPreviewConsole');
    expect(clientSource).toContain('callDevflowPreviewSubmitEdits');
    expect(clientSource).toContain('callDevflowPreviewRepairPrepare');
    expect(clientSource).toContain('unwrapDevflowPreviewHandlerResponse');
    expect(clientSource).toContain('VITE_DEVFLOW_PREVIEW_HANDLER_PATH');
    expect(clientSource).toContain('preview_start');
    expect(clientSource).toContain('preview_browser_open');
    expect(clientSource).toContain('preview_submit_edits');
    expect(typeSource).toContain('DevflowPreviewStartResult');
    expect(typeSource).toContain('DevflowPreviewSubmitEditsRequest');
    expect(typeSource).toContain("'doujia:submit-edits'");
  });

  it('adds preview acceptance as a workspace tab sibling of pipeline overview', () => {
    const runWorkspaceSource = readFileSync(
      join(root, 'client/src/pages/RunWorkspacePage/RunWorkspacePage.tsx'),
      'utf8',
    );
    const tabsSource = readFileSync(
      join(root, 'client/src/pages/RunWorkspacePage/WorkspaceTabs.tsx'),
      'utf8',
    );

    expect(runWorkspaceSource).toContain("type: 'preview-acceptance'");
    expect(runWorkspaceSource).toContain("title: '预览验收'");
    expect(tabsSource).toContain('PreviewAcceptancePanel');
    expect(tabsSource).toContain("case 'preview-acceptance'");
  });

  it('adds a final result tab that opens the generated bag preview', () => {
    const clientSource = readFileSync(join(root, 'client/src/api/devflow-client.ts'), 'utf8');
    const typeSource = readFileSync(join(root, 'shared/devflow-api.ts'), 'utf8');
    const sharedSource = readFileSync(join(root, 'shared/api.interface.ts'), 'utf8');
    const runWorkspaceSource = readFileSync(
      join(root, 'client/src/pages/RunWorkspacePage/RunWorkspacePage.tsx'),
      'utf8',
    );
    const tabsSource = readFileSync(
      join(root, 'client/src/pages/RunWorkspacePage/WorkspaceTabs.tsx'),
      'utf8',
    );
    const panelSource = readFileSync(
      join(root, 'client/src/pages/RunWorkspacePage/FinalResultPanel.tsx'),
      'utf8',
    );

    expect(typeSource).toContain('DevflowFinalResultOpenRequest');
    expect(typeSource).toContain('DevflowFinalResultOpenResult');
    expect(sharedSource).toContain('bagId?: string');
    expect(sharedSource).toContain('containerId?: string');
    expect(clientSource).toContain('callDevflowFinalResultOpen');
    expect(clientSource).toContain('final_result_open');
    expect(runWorkspaceSource).toContain("setActiveTabId('final-result')");
    expect(runWorkspaceSource).toContain("type: 'final-result'");
    expect(runWorkspaceSource).toContain("title: '最终结果'");
    expect(tabsSource).toContain('FinalResultPanel');
    expect(tabsSource).toContain("case 'final-result'");
    expect(panelSource).toContain('callDevflowFinalResultOpen');
    expect(panelSource).toContain('<iframe');
    expect(panelSource).toContain('bagId');
  });

  it('renders a host interface around selection and submit-edits contracts', () => {
    const panelSource = readFileSync(
      join(root, 'client/src/pages/RunWorkspacePage/PreviewAcceptancePanel.tsx'),
      'utf8',
    );

    expect(panelSource).toContain('preview_start');
    expect(panelSource).toContain('preview_browser_open');
    expect(panelSource).toContain('preview_inspector');
    expect(panelSource).toContain('preview_console');
    expect(panelSource).toContain('preview_submit_edits');
    expect(panelSource).toContain('preview_repair_prepare');
    expect(panelSource).toContain('doujia:selection');
    expect(panelSource).toContain('doujia:editor-mode');
    expect(panelSource).toContain('contentWindow?.addEventListener');
    expect(panelSource).toContain('contentWindow?.removeEventListener');
    expect(panelSource).toContain('set_text');
    expect(panelSource).toContain('set_style');
    expect(panelSource).toContain('set_layout');
  });
});
