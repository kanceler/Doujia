# Pipeline DAG UI Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the current horizontal Main pipeline cards with a reusable, dynamic, React Flow based DAG workflow view.

**Architecture:** Keep the current `RunWorkspacePage` data flow and replace only the pipeline visualization layer. Convert `DevflowPipelineWorkspaceView.main_pipeline_nodes/main_pipeline_edges` into frontend graph nodes and edges, lay them out with dagre, and render them with custom React Flow nodes plus a details panel.

**Tech Stack:** React 19, Vite, TypeScript, Tailwind CSS, lucide-react, `@xyflow/react`, `@dagrejs/dagre`.

---

## Current Entry Points

- Existing page mount: `client/src/pages/RunWorkspacePage/RunWorkspacePage.tsx`
- Existing pipeline component: `client/src/pages/RunWorkspacePage/PipelineView.tsx`
- Existing API client: `client/src/api/devflow-client.ts`
- Existing API data source: `GET /api/runs/:runId/pipeline-workspace`
- Existing shared types: `shared/devflow-api.ts`

The backend already returns `main_pipeline_nodes` and `main_pipeline_edges`, so this plan does not require backend changes for the first version.

## File Structure

- Create `client/src/components/pipeline/pipeline-types.ts`
  - Owns reusable graph node, edge, and status types.
- Create `client/src/components/pipeline/pipeline-status-style.ts`
  - Owns status normalization, colors, icons, labels, edge states, and duration formatting.
- Create `client/src/components/pipeline/pipeline-layout.ts`
  - Owns dagre layout and exposes a replaceable layout interface for future elkjs.
- Create `client/src/components/pipeline/pipeline-adapter.ts`
  - Converts existing `DevflowPipelineWorkspaceView` data into reusable graph input.
- Create `client/src/components/pipeline/PipelineNode.tsx`
  - Custom React Flow node UI.
- Create `client/src/components/pipeline/PipelineDetailsPanel.tsx`
  - Details panel for selected nodes.
- Create `client/src/components/pipeline/PipelineGraph.tsx`
  - React Flow canvas, header, stats, selection behavior, and details panel.
- Modify `client/src/pages/RunWorkspacePage/PipelineView.tsx`
  - Keep the current external props and delegate rendering to the reusable graph component.
- Add unit tests under `test/unit/pipeline-graph.spec.ts`
  - Validate adapter and layout behavior without browser rendering.

## Data Model

```ts
export type PipelineTaskStatus =
  | 'pending'
  | 'running'
  | 'completed'
  | 'failed'
  | 'kbug'
  | 'skipped';

export type PipelineEdgeStatus =
  | 'normal'
  | 'active'
  | 'blocked'
  | 'completed';

export interface PipelineTaskNode {
  id: string;
  title: string;
  role: string;
  op: string;
  status: PipelineTaskStatus;
  durationMs?: number;
  startedAt?: string;
  finishedAt?: string;
  artifactCount?: number;
  snapshotCount?: number;
  group?: string;
  taskId?: string;
  childPipelineInstanceId?: string;
  inputArtifacts?: string[];
  outputArtifacts?: string[];
  errorReason?: string;
}

export interface PipelineTaskEdge {
  id: string;
  source: string;
  target: string;
  status?: PipelineEdgeStatus;
}
```

## Implementation Tasks

### Task 1: Install Graph Dependencies

- [ ] Run `npm install @xyflow/react @dagrejs/dagre` from `code/`.
- [ ] Confirm `package.json` and `package-lock.json` include both packages.

### Task 2: Add Tests First

- [ ] Create `test/unit/pipeline-graph.spec.ts`.
- [ ] Test that adapter maps current `main_pipeline_nodes` and `main_pipeline_edges` into reusable graph data.
- [ ] Test that dagre layout returns finite `x/y` positions and preserves all input node ids.
- [ ] Run `npm test -- --runTestsByPath test/unit/pipeline-graph.spec.ts` and confirm the tests fail because the modules do not exist yet.

### Task 3: Add Data, Status, and Layout Modules

- [ ] Add graph types in `pipeline-types.ts`.
- [ ] Add status normalization and UI metadata in `pipeline-status-style.ts`.
- [ ] Add dagre layout in `pipeline-layout.ts`.
- [ ] Add API adapter in `pipeline-adapter.ts`.
- [ ] Run the focused unit test and confirm it passes.

### Task 4: Add Custom React Flow UI

- [ ] Create `PipelineNode.tsx` with icon, Chinese title, role/op line, status badge, duration, artifact button, and snapshot button.
- [ ] Create `PipelineDetailsPanel.tsx` with node metadata, input/output artifact lists, error reason, and open actions.
- [ ] Create `PipelineGraph.tsx` with header, canvas, stats footer, custom node type, smooth-step edges, selection state, fit view, and memoized layout.
- [ ] Ensure `PipelineGraph` accepts only `nodes`, `edges`, `selectedNodeId`, `onNodeSelect`, and optional display/action props.

### Task 5: Replace Existing PipelineView Internals

- [ ] Preserve `PipelineViewProps`.
- [ ] Use `toPipelineGraphData(workspace)` to generate graph nodes/edges.
- [ ] Pass `onOpenPipeline` and `onOpenTaskSnapshots` into `PipelineGraph`.
- [ ] Keep existing empty state behavior when there are no nodes.

### Task 6: Verify

- [ ] Run `npm test -- --runTestsByPath test/unit/pipeline-graph.spec.ts`.
- [ ] Run `npm run type:check:client`.
- [ ] Run `npm run build:client`.
- [ ] Start the client dev server if needed and inspect the workspace page in browser.

## Visual Direction

The UI should look like a modern SaaS workflow dashboard: quiet light background, white nodes, 8px radius, light border, restrained shadow, colored status accents, smooth connectors, and dense but readable metadata. It should not look like the default React Flow demo.

## Acceptance Criteria

- Current main pipeline renders from existing API data.
- `architect_write_plan` can fan out into `architect_create_container` and `split_module`.
- Node positions are generated by dagre, not hard-coded.
- Node selection opens a right details panel.
- Status colors cover `pending`, `running`, `completed`, `failed`, `kbug`, and `skipped`.
- Edges reflect completed, active, blocked, or normal states.
- The reusable component is independent from `RunWorkspacePage` business logic.
- The old `PipelineView` public props remain compatible with the current page.
