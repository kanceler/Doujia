import type {
  DevflowPipelineWorkspaceMainEdgeView,
  DevflowPipelineWorkspaceMainNodeView,
  DevflowPipelineWorkspaceView,
} from '@shared/devflow-api';
import type { PipelineGraphData, PipelineTaskEdge, PipelineTaskNode } from './pipeline-types';
import { deriveEdgeStatus, normalizePipelineStatus } from './pipeline-status-style';

export function toPipelineGraphData(workspace: DevflowPipelineWorkspaceView | null): PipelineGraphData {
  const rawNodes = workspace?.main_pipeline_nodes ?? [];
  const rawEdges = workspace?.main_pipeline_edges ?? [];
  const rawNodeById = new Map(rawNodes.map((node) => [node.node_id, node]));
  const graphNodes = rawNodes
    .filter(isVisibleTaskNode)
    .map(toPipelineTaskNode);
  const graphNodeById = new Map(graphNodes.map((node) => [node.id, node]));
  const graphEdges = simplifyPipelineEdges(rawEdges, rawNodeById, graphNodeById);

  return {
    nodes: graphNodes,
    edges: rawEdges.length > 0 ? graphEdges : buildFallbackSequenceEdges(graphNodes),
  };
}

function isVisibleTaskNode(node: DevflowPipelineWorkspaceMainNodeView): boolean {
  return node.node_kind !== 'fork' && node.node_kind !== 'join';
}

function toPipelineTaskNode(node: DevflowPipelineWorkspaceMainNodeView): PipelineTaskNode {
  const { role, op } = splitStageID(node.stage_id);
  return {
    id: node.node_id,
    title: node.label || node.stage_id,
    stageId: node.stage_id,
    role,
    op,
    status: normalizePipelineStatus(node.status),
    rawStatus: node.status,
    taskId: node.task_id,
    childPipelineInstanceId: node.child_pipeline_instance_id,
    artifactCount: undefined,
    snapshotCount: node.task_id ? undefined : 0,
    inputArtifacts: [],
    outputArtifacts: [],
  };
}

function splitStageID(stageID: string): { role: string; op: string } {
  const clean = stageID.trim();
  const override = STAGE_ROLE_OVERRIDES[clean];
  if (override) {
    return override;
  }
  const separator = clean.indexOf('_');
  if (separator <= 0) {
    return { role: 'pipeline', op: clean || 'task' };
  }
  const role = clean.slice(0, separator);
  const op = clean.slice(separator + 1);
  if (KNOWN_STAGE_ROLES.has(role)) {
    return { role, op };
  }
  return { role: 'pipeline', op: clean || 'task' };
}

const KNOWN_STAGE_ROLES = new Set(['ceo', 'pm', 'architect', 'coder', 'tester', 'front', 'human']);

const STAGE_ROLE_OVERRIDES: Record<string, { role: string; op: string }> = {
  split_module: { role: 'architect', op: 'split_module' },
  merge_code: { role: 'architect', op: 'merge_code' },
  global_test_code: { role: 'architect', op: 'global_test_code' },
  write_global_test_data: { role: 'tester', op: 'write_global_test_data' },
  test_all_modules: { role: 'pipeline', op: 'test_all_modules' },
  acceptance: { role: 'human', op: 'acceptance' },
};

function simplifyPipelineEdges(
  rawEdges: DevflowPipelineWorkspaceMainEdgeView[],
  rawNodeById: Map<string, DevflowPipelineWorkspaceMainNodeView>,
  graphNodeById: Map<string, PipelineTaskNode>,
): PipelineTaskEdge[] {
  const incomingByNode = groupEdges(rawEdges, 'to_node_id');
  const outgoingByNode = groupEdges(rawEdges, 'from_node_id');
  const edges: PipelineTaskEdge[] = [];
  const edgeIds = new Set<string>();

  for (const rawEdge of rawEdges) {
    const sources = resolveVisibleSources(rawEdge.from_node_id, rawNodeById, graphNodeById, incomingByNode);
    const targets = resolveVisibleTargets(rawEdge.to_node_id, rawNodeById, graphNodeById, outgoingByNode);
    for (const source of sources) {
      for (const target of targets) {
        if (source === target) {
          continue;
        }
        const id = `${source}->${target}`;
        if (edgeIds.has(id)) {
          continue;
        }
        const sourceNode = graphNodeById.get(source);
        const targetNode = graphNodeById.get(target);
        edges.push({
          id,
          source,
          target,
          status: deriveEdgeStatus(sourceNode?.status, targetNode?.status),
        });
        edgeIds.add(id);
      }
    }
  }

  return edges;
}

function groupEdges(
  edges: DevflowPipelineWorkspaceMainEdgeView[],
  key: 'from_node_id' | 'to_node_id',
): Map<string, DevflowPipelineWorkspaceMainEdgeView[]> {
  const grouped = new Map<string, DevflowPipelineWorkspaceMainEdgeView[]>();
  for (const edge of edges) {
    const id = edge[key];
    grouped.set(id, [...(grouped.get(id) ?? []), edge]);
  }
  return grouped;
}

function resolveVisibleSources(
  nodeID: string,
  rawNodeById: Map<string, DevflowPipelineWorkspaceMainNodeView>,
  graphNodeById: Map<string, PipelineTaskNode>,
  incomingByNode: Map<string, DevflowPipelineWorkspaceMainEdgeView[]>,
  visited = new Set<string>(),
): string[] {
  if (graphNodeById.has(nodeID)) {
    return [nodeID];
  }
  if (visited.has(nodeID)) {
    return [];
  }
  visited.add(nodeID);

  if (!rawNodeById.has(nodeID)) {
    return [];
  }

  const sources = new Set<string>();
  for (const edge of incomingByNode.get(nodeID) ?? []) {
    for (const source of resolveVisibleSources(edge.from_node_id, rawNodeById, graphNodeById, incomingByNode, visited)) {
      sources.add(source);
    }
  }
  return [...sources];
}

function resolveVisibleTargets(
  nodeID: string,
  rawNodeById: Map<string, DevflowPipelineWorkspaceMainNodeView>,
  graphNodeById: Map<string, PipelineTaskNode>,
  outgoingByNode: Map<string, DevflowPipelineWorkspaceMainEdgeView[]>,
  visited = new Set<string>(),
): string[] {
  if (graphNodeById.has(nodeID)) {
    return [nodeID];
  }
  if (visited.has(nodeID)) {
    return [];
  }
  visited.add(nodeID);

  if (!rawNodeById.has(nodeID)) {
    return [];
  }

  const targets = new Set<string>();
  for (const edge of outgoingByNode.get(nodeID) ?? []) {
    for (const target of resolveVisibleTargets(edge.to_node_id, rawNodeById, graphNodeById, outgoingByNode, visited)) {
      targets.add(target);
    }
  }
  return [...targets];
}

function buildFallbackSequenceEdges(nodes: PipelineTaskNode[]): PipelineTaskEdge[] {
  const edges: PipelineTaskEdge[] = [];
  for (let index = 1; index < nodes.length; index += 1) {
    const source = nodes[index - 1];
    const target = nodes[index];
    edges.push({
      id: `${source.id}->${target.id}`,
      source: source.id,
      target: target.id,
      status: deriveEdgeStatus(source.status, target.status),
    });
  }
  return edges;
}
