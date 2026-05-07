import type {
  DevflowPipelineWorkspaceView,
  DevflowWorkspaceOverviewView,
} from '../../shared/devflow-api';
import { buildPipelineOverviewItems } from '../../client/src/pages/RunWorkspacePage/pipeline-overview-items';

describe('pipeline overview items', () => {
  it('uses the original real overview and pipeline workspace rows', () => {
    const workspaceOverview = {
      current_iteration_no: 1,
      acceptance_checkpoint: {
        checkpoint_task_id: 'acceptance_iter_01',
      },
      iterations: [
        {
          iteration_no: 1,
          status: 'completed',
        },
      ],
      session_artifacts: [
        {
          artifact_id: 'artifact_memory',
          kind: 'conversation_memory',
          title: 'Doujia Memory',
          content: 'memory content',
        },
        {
          artifact_id: 'artifact_summary',
          kind: 'requirement_summary',
          title: 'Snake game',
          content: 'Create a snake game',
        },
      ],
    } as DevflowWorkspaceOverviewView;
    const pipelineWorkspace = {
      instances: [
        {
          instance_id: 'root',
          pipeline_id: 'phase_two_delivery_flow',
          status: 'completed',
        },
        {
          instance_id: 'module01',
          pipeline_id: 'pipeline_module',
          status: 'completed',
        },
      ],
    } as DevflowPipelineWorkspaceView;

    expect(buildPipelineOverviewItems(workspaceOverview, pipelineWorkspace)).toEqual([
      { id: 'current-iteration', label: 'Current iteration', value: 'Iteration 1' },
      {
        id: 'acceptance-acceptance_iter_01',
        label: 'Acceptance checkpoint',
        value: 'acceptance_iter_01',
      },
      { id: 'iteration-1', label: 'Iteration 1', value: 'completed', badge: true },
      {
        id: 'artifact-artifact_memory',
        label: 'conversation_memory',
        value: 'Doujia Memory',
      },
      {
        id: 'artifact-artifact_summary',
        label: 'requirement_summary',
        value: 'Snake game',
      },
      {
        id: 'instance-root',
        label: 'phase_two_delivery_flow',
        value: 'completed',
        badge: true,
      },
      {
        id: 'instance-module01',
        label: 'pipeline_module',
        value: 'completed',
        badge: true,
      },
    ]);
  });
});
