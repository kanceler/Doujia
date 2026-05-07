import { readFileSync } from 'node:fs';
import { join } from 'node:path';

const root = join(__dirname, '..', '..');

describe('client devflow API wiring', () => {
  it('keeps the home page on the Go DevFlow API client', () => {
    const source = readFileSync(join(root, 'client/src/pages/HomePage/HomePage.tsx'), 'utf8');
    expect(source).toContain("from '@/api/devflow-client'");
    expect(source).toContain('getDevflowDemoRuns');
    expect(source).not.toContain("from '@client/src/api'");
  });

  it('keeps local app bootstrap independent from the old backend shell', () => {
    const source = readFileSync(join(root, 'client/src/index.tsx'), 'utf8');
    expect(source).not.toContain('AppContainer');
    expect(source).toContain('应用加载失败');
    expect(source).toContain('重试');
  });

  it('keeps the Vite dev server pointed at the Go API', () => {
    const source = readFileSync(join(root, 'vite.config.ts'), 'utf8');
    expect(source).toContain('clientSpaFallbackPlugin');
    expect(source).toContain('127.0.0.1:18080');
    expect(source).toContain("'/api'");
  });

  it('wires workspace checkpoint actions through the Go client', () => {
    const clientSource = readFileSync(join(root, 'client/src/api/devflow-client.ts'), 'utf8');
    const approvalSource = readFileSync(join(root, 'client/src/pages/RunWorkspacePage/ApprovalCard.tsx'), 'utf8');
    expect(clientSource).toContain('approveDevflowCheckpoint');
    expect(clientSource).toContain('rejectDevflowCheckpoint');
    expect(approvalSource).toContain("from '@/api/devflow-client'");
  });

  it('routes node artifact previews through the Go client', () => {
    const clientSource = readFileSync(join(root, 'client/src/api/devflow-client.ts'), 'utf8');
    const panelSource = readFileSync(join(root, 'client/src/pages/RunWorkspacePage/NodeDetailPanel.tsx'), 'utf8');
    expect(clientSource).toContain('getDevflowArtifactContent');
    expect(panelSource).toContain('getDevflowArtifactContent');
  });

  it('keeps run creation usable when the legacy clarification plugin is unavailable', () => {
    const source = readFileSync(join(root, 'client/src/pages/RunCreatePage/RunCreatePage.tsx'), 'utf8');
    expect(source).toContain('DEFAULT_TARGET_REPO');
    expect(source).toContain('createDevflowProject');
    expect(source).toContain('createDevflowRun');
    expect(source).toContain('Requirement chat and confirmation now happen in the workspace');
  });

  it('surfaces requirement summary actions before creating a run', () => {
    const source = readFileSync(join(root, 'client/src/pages/RunCreatePage/RunCreatePage.tsx'), 'utf8');
    expect(source).toContain('handleValidateAndLaunch');
    expect(source).toContain('handleConfirmLaunch');
    expect(source).toContain('start_immediately: true');
    expect(source).toContain('navigate(`/run/${result.run_id}/workspace`)');
  });

  it('keeps launch failures visible', () => {
    const source = readFileSync(join(root, 'client/src/pages/RunCreatePage/RunCreatePage.tsx'), 'utf8');
    expect(source).toContain('const [launchError, setLaunchError] = useState');
    expect(source).toContain('setLaunchError(null);');
  });

  it('keeps the browser shell and workspace empty state readable in Chinese', () => {
    const htmlSource = readFileSync(join(root, 'client/index.html'), 'utf8');
    const indexSource = readFileSync(join(root, 'client/src/index.tsx'), 'utf8');
    const messageFlowSource = readFileSync(join(root, 'client/src/pages/RunWorkspacePage/MessageFlow.tsx'), 'utf8');
    expect(htmlSource).toContain('Doujia');
    expect(indexSource).toContain('应用加载失败');
    expect(messageFlowSource).toContain('等待你和 Doujia 对话');
    expect(messageFlowSource).toContain('系统事件已经移动到 Pipeline 和节点详情；这里只保留你需要参与的对话、确认和需求补充。');
  });

  it('passes frontend LLM settings into the Go run creation request', () => {
    const typesSource = readFileSync(join(root, 'shared/devflow-api.ts'), 'utf8');
    const createSource = readFileSync(join(root, 'client/src/pages/RunCreatePage/RunCreatePage.tsx'), 'utf8');
    expect(typesSource).toContain('model_provider?: string');
    expect(typesSource).toContain('model_name?: string');
    expect(typesSource).toContain('api_key?: string');
    expect(createSource).toContain('model_provider: config.modelProvider');
    expect(createSource).toContain('api_style: config.apiStyle');
  });

  it('lets users choose the OpenAI-compatible API style', () => {
    const sharedSource = readFileSync(join(root, 'shared/api.interface.ts'), 'utf8');
    const configSource = readFileSync(join(root, 'client/src/pages/RunCreatePage/ConfigPanel.tsx'), 'utf8');
    expect(sharedSource).toContain('apiStyle:');
    expect(configSource).toContain("updateField('apiStyle'");
    expect(configSource).toContain('chat_completions');
    expect(configSource).toContain('responses');
  });

  it('uses readable project navigation labels', () => {
    const source = readFileSync(join(root, 'client/src/components/Layout.tsx'), 'utf8');
    expect(source).toContain("label: '新项目'");
    expect(source).toContain("label: '导入 doujia-git'");
    expect(source).toContain("label: '我的应用'");
    expect(source).toContain("label: '插件'");
    expect(source).toContain('最近的项目');
  });

  it('uses the first user message as the recent run title', () => {
    const source = readFileSync(join(root, 'client/src/components/Layout.tsx'), 'utf8');
    expect(source).toContain('getDevflowSessionMessages');
    expect(source).toContain("message.role === 'user'");
    expect(source).toContain('title: firstUserMessage?.content.trim() || run.run_id');
    expect(source).toContain('getRunStatusLabel');
  });

  it('keeps the workspace chat center focused on human dialogue', () => {
    const source = readFileSync(join(root, 'client/src/pages/RunWorkspacePage/RunWorkspacePage.tsx'), 'utf8');
    expect(source).toContain('toDialogMessages');
    expect(source).toContain('sessionMessages');
    expect(source).not.toContain('eventMessages');
  });

  it('shows a clickable generated-project card when the run is complete', () => {
    const sharedSource = readFileSync(join(root, 'shared/api.interface.ts'), 'utf8');
    const workspaceSource = readFileSync(join(root, 'client/src/pages/RunWorkspacePage/RunWorkspacePage.tsx'), 'utf8');
    const messageFlowSource = readFileSync(join(root, 'client/src/pages/RunWorkspacePage/MessageFlow.tsx'), 'utf8');

    expect(sharedSource).toContain("'project-generated'");
    expect(workspaceSource).toContain("message.message_type === 'project_generated'");
    expect(workspaceSource).toContain("run?.status === 'completed'");
    expect(workspaceSource).toContain("id: 'generated-project-card'");
    expect(messageFlowSource).toContain('ProjectGeneratedCard');
    expect(messageFlowSource).toContain("case 'project-generated'");
    expect(messageFlowSource).toContain('onProjectGeneratedClick');
  });

  it('wires the iterative delivery APIs through the Go client', () => {
    const source = readFileSync(join(root, 'client/src/api/devflow-client.ts'), 'utf8');
    expect(source).toContain('postDevflowSessionMessage');
    expect(source).toContain('postDevflowSessionMessageStream');
    expect(source).toContain('/session/messages/stream');
    expect(source).toContain('confirmDevflowRequirementSummary');
    expect(source).toContain('getDevflowProjects');
    expect(source).toContain('createDevflowProject');
    expect(source).toContain('getDevflowPipelineWorkspace');
    expect(source).toContain('/pipeline-workspace');
  });

  it('streams Doujia chat into a temporary workspace message before refreshing persisted messages', () => {
    const source = readFileSync(join(root, 'client/src/pages/RunWorkspacePage/RunWorkspacePage.tsx'), 'utf8');
    expect(source).toContain('streamingAssistantMessage');
    expect(source).toContain('postDevflowSessionMessageStream');
    expect(source).toContain('onDelta');
    expect(source).toContain('streamingAssistantMessage ? [...messages, streamingAssistantMessage] : messages');
  });

  it('adds project binding and project llm fields to the Go run creation contract', () => {
    const typesSource = readFileSync(join(root, 'shared/devflow-api.ts'), 'utf8');
    expect(typesSource).toContain('project_id?: string');
    expect(typesSource).toContain('export interface DevflowProjectView');
  });

  it('defaults the pipeline panel to roughly half the viewport while keeping resize support', () => {
    const source = readFileSync(join(root, 'client/src/pages/RunWorkspacePage/RunWorkspacePage.tsx'), 'utf8');
    expect(source).toContain('getInitialPipelineWidth');
    expect(source).toContain('window.innerWidth * 0.5');
    expect(source).toContain('clampPipelineWidth');
  });

  it('explains that the workspace message area is reserved for human participation', () => {
    const source = readFileSync(join(root, 'client/src/pages/RunWorkspacePage/MessageFlow.tsx'), 'utf8');
    expect(source).toContain('Doujia');
    expect(source).toContain('Pipeline');
  });

  it('removes the unused current project rail from the workspace', () => {
    const source = readFileSync(join(root, 'client/src/pages/RunWorkspacePage/RunWorkspacePage.tsx'), 'utf8');
    expect(source).not.toContain('WorkspaceLeftNav');
    expect(source).not.toContain('w-[200px]');
  });

  it('applies the default 75 percent viewport scale globally', () => {
    const source = readFileSync(join(root, 'client/src/index.css'), 'utf8');
    expect(source).toContain('--doujia-default-viewport-scale: 0.75');
    expect(source).toContain('font-size: calc(16px * var(--doujia-default-viewport-scale))');
  });

  it('locks the workspace shell to viewport height so only the message pane scrolls', () => {
    const layoutSource = readFileSync(join(root, 'client/src/components/Layout.tsx'), 'utf8');
    const workspaceSource = readFileSync(join(root, 'client/src/pages/RunWorkspacePage/RunWorkspacePage.tsx'), 'utf8');
    const messageFlowSource = readFileSync(join(root, 'client/src/pages/RunWorkspacePage/MessageFlow.tsx'), 'utf8');
    expect(layoutSource).toContain('h-screen overflow-hidden');
    expect(layoutSource).toContain('min-h-0 flex-1 overflow-hidden');
    expect(workspaceSource).toContain('flex h-full min-h-0 overflow-hidden');
    expect(messageFlowSource).toContain('<ScrollArea ref={scrollRef} className="h-full">');
  });
});
