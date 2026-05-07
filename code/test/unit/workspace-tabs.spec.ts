import { readFileSync } from 'node:fs';
import { join } from 'node:path';

const root = join(__dirname, '..', '..');

describe('workspace tabs and pipeline workspace', () => {
  it('switches the workspace page to backend-driven pipeline workspace and recursive tabs', () => {
    const runWorkspaceSource = readFileSync(
      join(root, 'client/src/pages/RunWorkspacePage/RunWorkspacePage.tsx'),
      'utf8',
    );
    const tabsSource = readFileSync(
      join(root, 'client/src/pages/RunWorkspacePage/WorkspaceTabs.tsx'),
      'utf8',
    );

    expect(runWorkspaceSource).toContain('getDevflowPipelineWorkspace');
    expect(runWorkspaceSource).toContain('getDevflowWorkspaceOverview');
    expect(runWorkspaceSource).toContain('getDevflowPipelineGraph');
    expect(runWorkspaceSource).toContain('WorkspaceTabs');
    expect(runWorkspaceSource).toContain('PipelineView');
    expect(runWorkspaceSource).toContain('buildPipelineHomeTab');
    expect(runWorkspaceSource).toContain("type: 'pipeline-home'");
    expect(runWorkspaceSource).toContain("type: 'pipeline-graph'");
    expect(runWorkspaceSource).toContain("type: 'node-snapshots'");
    expect(runWorkspaceSource).toContain('getDevflowPipelineWorkspace(runId, instanceId)');
    expect(tabsSource).toContain('pipeline-home');
    expect(tabsSource).toContain('node-detail');
    expect(tabsSource).toContain('pipeline-graph');
    expect(tabsSource).toContain('node-snapshots');
  });

  it('renders the top graph through the reusable React Flow DAG component', () => {
    const pipelineSource = readFileSync(
      join(root, 'client/src/pages/RunWorkspacePage/PipelineView.tsx'),
      'utf8',
    );
    const graphSource = readFileSync(
      join(root, 'client/src/components/pipeline/PipelineGraph.tsx'),
      'utf8',
    );
    const composerSource = readFileSync(
      join(root, 'client/src/pages/RunWorkspacePage/SessionComposer.tsx'),
      'utf8',
    );

    expect(pipelineSource).toContain('main_pipeline_nodes');
    expect(pipelineSource).toContain('main_pipeline_edges');
    expect(pipelineSource).toContain('toPipelineGraphData');
    expect(pipelineSource).toContain('PipelineGraph');
    expect(graphSource).toContain('ReactFlow');
    expect(graphSource).toContain('nodeTypes');
    expect(graphSource).toContain('smoothstep');
    expect(graphSource).toContain('PipelineDetailsPanel');
    expect(pipelineSource).toContain('Main pipeline not ready yet.');
    expect(composerSource).toContain('继续和 Doujia 对话');
  });

  it('renders requirement summary confirmation actions in the message flow', () => {
    const runWorkspaceSource = readFileSync(
      join(root, 'client/src/pages/RunWorkspacePage/RunWorkspacePage.tsx'),
      'utf8',
    );
    const messageFlowSource = readFileSync(
      join(root, 'client/src/pages/RunWorkspacePage/MessageFlow.tsx'),
      'utf8',
    );

    expect(runWorkspaceSource).toContain('confirmDevflowRequirementSummary');
    expect(runWorkspaceSource).toContain("message.message_type === 'requirement_summary'");
    expect(messageFlowSource).toContain('RequirementSummaryCard');
    expect(messageFlowSource).toContain("case 'requirement-summary'");
  });

  it('keeps the pipeline overview as a permanent browser-like home tab', () => {
    const runWorkspaceSource = readFileSync(
      join(root, 'client/src/pages/RunWorkspacePage/RunWorkspacePage.tsx'),
      'utf8',
    );
    const tabsSource = readFileSync(
      join(root, 'client/src/pages/RunWorkspacePage/WorkspaceTabs.tsx'),
      'utf8',
    );

    expect(runWorkspaceSource).toContain('buildPipelineHomeTab');
    expect(runWorkspaceSource).toContain('setTabs((current) => upsertWorkspaceTab(current, buildPipelineHomeTab');
    expect(runWorkspaceSource).toContain("activeTabId ?? 'pipeline-home'");
    expect(tabsSource).toContain('PipelineHomePanel');
    expect(tabsSource).toContain("tab.type !== 'pipeline-home'");
  });

  it('supports focused workspace fetches for recursive pipeline tabs', () => {
    const clientSource = readFileSync(
      join(root, 'client/src/api/devflow-client.ts'),
      'utf8',
    );
    expect(clientSource).toContain('focus_instance_id');
  });

  it('uses the opened child pipeline tab title inside the graph header', () => {
    const pipelineSource = readFileSync(
      join(root, 'client/src/pages/RunWorkspacePage/PipelineView.tsx'),
      'utf8',
    );
    const tabsSource = readFileSync(
      join(root, 'client/src/pages/RunWorkspacePage/WorkspaceTabs.tsx'),
      'utf8',
    );

    expect(pipelineSource).toContain('title?: string');
    expect(pipelineSource).toContain("title = 'Main pipeline'");
    expect(pipelineSource).toContain('const graphTitle = title.trim()');
    expect(pipelineSource).toContain('key={graphTitle}');
    expect(pipelineSource).toContain('title={graphTitle}');
    expect(tabsSource).toContain('title={tab.title}');
    expect(tabsSource).toContain('key={tab.id}');
  });
});
