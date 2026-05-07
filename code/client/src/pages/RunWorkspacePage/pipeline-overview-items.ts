import type {
  DevflowPipelineWorkspaceView,
  DevflowSessionArtifactView,
  DevflowWorkspaceOverviewView,
} from '@shared/devflow-api';

export interface OverviewFieldItem {
  id: string;
  label: string;
  value: string;
  badge?: boolean;
}

function artifactValue(artifact: DevflowSessionArtifactView): string {
  return artifact.title || artifact.content || artifact.artifact_id;
}

export function buildPipelineOverviewItems(
  workspaceOverview?: DevflowWorkspaceOverviewView | null,
  pipelineWorkspace?: DevflowPipelineWorkspaceView | null,
): OverviewFieldItem[] {
  const items: OverviewFieldItem[] = [];
  const currentIteration =
    workspaceOverview?.current_iteration_no ?? pipelineWorkspace?.current_iteration_no;

  if (currentIteration) {
    items.push({
      id: 'current-iteration',
      label: 'Current iteration',
      value: `Iteration ${currentIteration}`,
    });
  }

  const checkpoint = workspaceOverview?.acceptance_checkpoint;
  if (checkpoint?.checkpoint_task_id) {
    items.push({
      id: `acceptance-${checkpoint.checkpoint_task_id}`,
      label: 'Acceptance checkpoint',
      value: checkpoint.checkpoint_task_id,
    });
  }

  for (const iteration of workspaceOverview?.iterations ?? pipelineWorkspace?.iterations ?? []) {
    items.push({
      id: `iteration-${iteration.iteration_no}`,
      label: `Iteration ${iteration.iteration_no}`,
      value: iteration.status,
      badge: true,
    });
  }

  for (const artifact of workspaceOverview?.session_artifacts ?? []) {
    items.push({
      id: `artifact-${artifact.artifact_id}`,
      label: artifact.kind,
      value: artifactValue(artifact),
    });
  }

  for (const instance of pipelineWorkspace?.instances ?? []) {
    items.push({
      id: `instance-${instance.instance_id}`,
      label: instance.pipeline_id,
      value: instance.status,
      badge: true,
    });
  }

  return items;
}
