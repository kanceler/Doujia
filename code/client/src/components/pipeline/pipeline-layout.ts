import * as dagre from '@dagrejs/dagre';
import type { PipelineLayoutResult, PipelineTaskEdge, PipelineTaskNode } from './pipeline-types';

const NODE_WIDTH = 196;
const NODE_HEIGHT = 112;
const RANK_SEPARATION = 92;
const NODE_SEPARATION = 58;

export function layoutPipelineGraph(
  nodes: PipelineTaskNode[],
  edges: PipelineTaskEdge[],
): PipelineLayoutResult {
  const graph = new dagre.graphlib.Graph();
  graph.setDefaultEdgeLabel(() => ({}));
  graph.setGraph({
    rankdir: 'LR',
    align: 'UL',
    ranksep: RANK_SEPARATION,
    nodesep: NODE_SEPARATION,
    marginx: 28,
    marginy: 28,
  });

  for (const node of nodes) {
    graph.setNode(node.id, { width: NODE_WIDTH, height: NODE_HEIGHT });
  }
  for (const edge of edges) {
    graph.setEdge(edge.source, edge.target);
  }

  dagre.layout(graph);

  return {
    nodes: nodes.map((node) => {
      const position = graph.node(node.id) as { x?: number; y?: number } | undefined;
      return {
        ...node,
        position: {
          x: (position?.x ?? 0) - NODE_WIDTH / 2,
          y: (position?.y ?? 0) - NODE_HEIGHT / 2,
        },
      };
    }),
    edges,
  };
}
