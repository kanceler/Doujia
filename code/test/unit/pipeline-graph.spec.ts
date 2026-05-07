import type { DevflowPipelineWorkspaceView } from '../../shared/devflow-api';
import { layoutPipelineGraph } from '../../client/src/components/pipeline/pipeline-layout';
import { toPipelineGraphData } from '../../client/src/components/pipeline/pipeline-adapter';
import {
  summarizePipeline,
  summarizeStatus,
} from '../../client/src/components/pipeline/pipeline-status-style';

const workspace: DevflowPipelineWorkspaceView = {
  run_id: 'run_pipeline_graph_test',
  status: 'running',
  current_iteration_no: 1,
  iterations: [],
  instances: [],
  edges: [],
  main_pipeline_nodes: [
    {
      node_id: 'task:ceo_write_requirement',
      node_kind: 'task',
      stage_id: 'ceo_write_requirement',
      label: 'Doujia 写需求',
      status: 'completed',
      task_id: 'ceo_write_requirement',
    },
    {
      node_id: 'task:pm_write_plan',
      node_kind: 'task',
      stage_id: 'pm_write_plan',
      label: '产品经理写 PRD',
      status: 'completed',
      task_id: 'pm_write_plan',
    },
    {
      node_id: 'task:architect_write_plan',
      node_kind: 'task',
      stage_id: 'architect_write_plan',
      label: '架构师写架构书',
      status: 'completed',
      task_id: 'architect_write_plan',
    },
    {
      node_id: 'fork:prepare_delivery',
      node_kind: 'fork',
      stage_id: 'fork_prepare_delivery',
      label: '并行准备',
      status: 'running',
    },
    {
      node_id: 'task:architect_create_container',
      node_kind: 'task',
      stage_id: 'architect_create_container',
      label: '创建容器',
      status: 'completed',
      task_id: 'architect_create_container',
    },
    {
      node_id: 'task:split_module',
      node_kind: 'task',
      stage_id: 'split_module',
      label: '拆分模块',
      status: 'running',
      task_id: 'split_module',
    },
    {
      node_id: 'join:prepare_delivery',
      node_kind: 'join',
      stage_id: 'join_prepare_delivery',
      label: '准备完成',
      status: 'running',
    },
    {
      node_id: 'task:architect_merge_code',
      node_kind: 'task',
      stage_id: 'architect_merge_code',
      label: '合并代码',
      status: 'not_started',
      task_id: 'architect_merge_code',
    },
  ],
  main_pipeline_edges: [
    { from_node_id: 'task:ceo_write_requirement', to_node_id: 'task:pm_write_plan', kind: 'sequence' },
    { from_node_id: 'task:pm_write_plan', to_node_id: 'task:architect_write_plan', kind: 'sequence' },
    { from_node_id: 'task:architect_write_plan', to_node_id: 'fork:prepare_delivery', kind: 'sequence' },
    { from_node_id: 'fork:prepare_delivery', to_node_id: 'task:architect_create_container', kind: 'fork' },
    { from_node_id: 'fork:prepare_delivery', to_node_id: 'task:split_module', kind: 'fork' },
    { from_node_id: 'task:architect_create_container', to_node_id: 'join:prepare_delivery', kind: 'join' },
    { from_node_id: 'task:split_module', to_node_id: 'join:prepare_delivery', kind: 'join' },
    { from_node_id: 'join:prepare_delivery', to_node_id: 'task:architect_merge_code', kind: 'sequence' },
  ],
};

describe('pipeline graph adapter and layout', () => {
  it('converts backend workspace data into task-only graph data with fork joins simplified', () => {
    const graph = toPipelineGraphData(workspace);

    expect(graph.nodes.map((node) => node.id)).toEqual([
      'task:ceo_write_requirement',
      'task:pm_write_plan',
      'task:architect_write_plan',
      'task:architect_create_container',
      'task:split_module',
      'task:architect_merge_code',
    ]);
    expect(graph.nodes.find((node) => node.id === 'task:architect_create_container')).toMatchObject({
      role: 'architect',
      op: 'create_container',
      status: 'completed',
      taskId: 'architect_create_container',
    });
    expect(graph.nodes.find((node) => node.id === 'task:split_module')).toMatchObject({
      role: 'architect',
      op: 'split_module',
      status: 'running',
    });
    expect(graph.edges.map((edge) => `${edge.source}->${edge.target}`)).toEqual([
      'task:ceo_write_requirement->task:pm_write_plan',
      'task:pm_write_plan->task:architect_write_plan',
      'task:architect_write_plan->task:architect_create_container',
      'task:architect_write_plan->task:split_module',
      'task:architect_create_container->task:architect_merge_code',
      'task:split_module->task:architect_merge_code',
    ]);
  });

  it('lays out every graph node with finite positions', () => {
    const graph = toPipelineGraphData(workspace);
    const laidOut = layoutPipelineGraph(graph.nodes, graph.edges);

    expect(laidOut.nodes).toHaveLength(graph.nodes.length);
    expect(laidOut.edges).toHaveLength(graph.edges.length);
    for (const node of laidOut.nodes) {
      expect(Number.isFinite(node.position.x)).toBe(true);
      expect(Number.isFinite(node.position.y)).toBe(true);
    }
  });

  it('derives the graph badge status from visible nodes instead of the run acceptance status', () => {
    const graph = toPipelineGraphData({
      ...workspace,
      status: 'awaiting_acceptance',
      main_pipeline_nodes: [
        {
          node_id: 'task:acceptance_iter_01',
          node_kind: 'task',
          stage_id: 'acceptance',
          label: 'Acceptance',
          status: 'completed',
          task_id: 'acceptance_iter_01',
        },
      ],
      main_pipeline_edges: [],
    });

    const summary = summarizePipeline(graph.nodes);

    expect(summary).toMatchObject({
      total: 1,
      completed: 1,
      running: 0,
      pending: 0,
    });
    expect(summarizeStatus(summary, 'awaiting_acceptance')).toBe('completed');
  });

  it('does not invent a sequence edge when focused parallel nodes only connect through hidden fork joins', () => {
    const graph = toPipelineGraphData({
      ...workspace,
      main_pipeline_nodes: [
        {
          node_id: 'fork:root_test_all_modules_module01:module_work',
          node_kind: 'fork',
          stage_id: 'fork_module_work',
          label: '模块并行准备',
          status: 'running',
        },
        {
          node_id: 'child:write_code:root_test_all_modules_module01_write_code_single',
          node_kind: 'child_pipeline',
          stage_id: 'write_code',
          label: '编写代码',
          status: 'running',
          child_pipeline_instance_id: 'root_test_all_modules_module01_write_code_single',
        },
        {
          node_id: 'child:write_test_data:root_test_all_modules_module01_write_test_data_single',
          node_kind: 'child_pipeline',
          stage_id: 'write_test_data',
          label: '编写测试数据',
          status: 'completed',
          child_pipeline_instance_id: 'root_test_all_modules_module01_write_test_data_single',
        },
        {
          node_id: 'join:root_test_all_modules_module01:module_test_input',
          node_kind: 'join',
          stage_id: 'join_module_test_input',
          label: '模块测试输入就绪',
          status: 'running',
        },
      ],
      main_pipeline_edges: [
        {
          from_node_id: 'fork:root_test_all_modules_module01:module_work',
          to_node_id: 'child:write_code:root_test_all_modules_module01_write_code_single',
          kind: 'fork',
        },
        {
          from_node_id: 'fork:root_test_all_modules_module01:module_work',
          to_node_id: 'child:write_test_data:root_test_all_modules_module01_write_test_data_single',
          kind: 'fork',
        },
        {
          from_node_id: 'child:write_code:root_test_all_modules_module01_write_code_single',
          to_node_id: 'join:root_test_all_modules_module01:module_test_input',
          kind: 'join',
        },
        {
          from_node_id: 'child:write_test_data:root_test_all_modules_module01_write_test_data_single',
          to_node_id: 'join:root_test_all_modules_module01:module_test_input',
          kind: 'join',
        },
      ],
    });

    expect(graph.nodes.map((node) => node.id)).toEqual([
      'child:write_code:root_test_all_modules_module01_write_code_single',
      'child:write_test_data:root_test_all_modules_module01_write_test_data_single',
    ]);
    expect(graph.edges).toEqual([]);
  });
});
