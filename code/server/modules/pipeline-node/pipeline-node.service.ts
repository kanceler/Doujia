import { Injectable, Inject, NotFoundException } from '@nestjs/common';
import { Logger } from '@nestjs/common';
import { DRIZZLE_DATABASE, type PostgresJsDatabase } from '@lark-apaas/fullstack-nestjs-core';
import { eq, asc } from 'drizzle-orm';
import { pipelineNodeTable } from '@server/database/schema';
import type {
  PipelineNodeItem,
  PipelineNodeDetail,
  PipelineHistoryResponse,
  PipelineHistoryNode,
  PipelineHistoryEdge,
  RefHistory,
  NodeSnapshot,
  NodeArtifact,
} from '@shared/api.interface';

@Injectable()
export class PipelineNodeService {
  private readonly logger = new Logger(PipelineNodeService.name);

  constructor(
    @Inject(DRIZZLE_DATABASE) private readonly db: PostgresJsDatabase,
  ) {}

  async getByRunId(runId: string): Promise<{ items: PipelineNodeItem[] }> {
    const rows: {
      id: string;
      runId: string;
      name: string;
      type: string;
      status: string;
      parentId: string | null;
      position: number;
      ref: string | null;
      createdAt: Date;
    }[] = await this.db
      .select({
        id: pipelineNodeTable.id,
        runId: pipelineNodeTable.runId,
        name: pipelineNodeTable.name,
        type: pipelineNodeTable.type,
        status: pipelineNodeTable.status,
        parentId: pipelineNodeTable.parentId,
        position: pipelineNodeTable.position,
        ref: pipelineNodeTable.ref,
        createdAt: pipelineNodeTable.createdAt,
      })
      .from(pipelineNodeTable)
      .where(eq(pipelineNodeTable.runId, runId))
      .orderBy(asc(pipelineNodeTable.position));

    const items: PipelineNodeItem[] = rows.map((row: {
      id: string;
      runId: string;
      name: string;
      type: string;
      status: string;
      parentId: string | null;
      position: number;
      ref: string | null;
      createdAt: Date;
    }) => ({
      id: row.id,
      runId: row.runId,
      name: row.name,
      type: row.type as PipelineNodeItem['type'],
      status: row.status as PipelineNodeItem['status'],
      parentId: row.parentId,
      position: row.position,
      ref: row.ref,
      createdAt: row.createdAt.toISOString(),
    }));

    return { items };
  }

  async getById(nodeId: string): Promise<{
    id: string;
    runId: string;
    name: string;
    type: string;
    status: string;
    parentId: string | null;
    position: number;
    snapshot: unknown;
    artifact: unknown;
    ref: string | null;
    createdAt: Date;
  } | null> {
    const rows: {
      id: string;
      runId: string;
      name: string;
      type: string;
      status: string;
      parentId: string | null;
      position: number;
      snapshot: unknown;
      artifact: unknown;
      ref: string | null;
      createdAt: Date;
    }[] = await this.db
      .select({
        id: pipelineNodeTable.id,
        runId: pipelineNodeTable.runId,
        name: pipelineNodeTable.name,
        type: pipelineNodeTable.type,
        status: pipelineNodeTable.status,
        parentId: pipelineNodeTable.parentId,
        position: pipelineNodeTable.position,
        snapshot: pipelineNodeTable.snapshot,
        artifact: pipelineNodeTable.artifact,
        ref: pipelineNodeTable.ref,
        createdAt: pipelineNodeTable.createdAt,
      })
      .from(pipelineNodeTable)
      .where(eq(pipelineNodeTable.id, nodeId));

    if (rows.length === 0) {
      return null;
    }
    return rows[0];
  }

  async getNodeDetail(nodeId: string): Promise<PipelineNodeDetail> {
    const row = await this.getById(nodeId);
    if (!row) {
      throw new NotFoundException(`Pipeline node ${nodeId} not found`);
    }

    const snapshot: NodeSnapshot | null = row.snapshot
      ? (row.snapshot as NodeSnapshot)
      : null;
    const artifact: NodeArtifact | null = row.artifact
      ? (row.artifact as NodeArtifact)
      : null;

    return {
      id: row.id,
      name: row.name,
      type: row.type as PipelineNodeDetail['type'],
      status: row.status as PipelineNodeDetail['status'],
      snapshot,
      artifact,
      approvalRecords: [],
      ref: row.ref,
      createdAt: row.createdAt.toISOString(),
    };
  }

  async getPipelineHistory(runId: string): Promise<PipelineHistoryResponse> {
    const rows: {
      id: string;
      runId: string;
      name: string;
      type: string;
      status: string;
      parentId: string | null;
      position: number;
      ref: string | null;
      createdAt: Date;
    }[] = await this.db
      .select({
        id: pipelineNodeTable.id,
        runId: pipelineNodeTable.runId,
        name: pipelineNodeTable.name,
        type: pipelineNodeTable.type,
        status: pipelineNodeTable.status,
        parentId: pipelineNodeTable.parentId,
        position: pipelineNodeTable.position,
        ref: pipelineNodeTable.ref,
        createdAt: pipelineNodeTable.createdAt,
      })
      .from(pipelineNodeTable)
      .where(eq(pipelineNodeTable.runId, runId))
      .orderBy(asc(pipelineNodeTable.position));

    const nodes: PipelineHistoryNode[] = rows.map((row: {
      id: string;
      runId: string;
      name: string;
      type: string;
      status: string;
      parentId: string | null;
      position: number;
      ref: string | null;
      createdAt: Date;
    }) => ({
      id: row.id,
      name: row.name,
      type: row.type as PipelineHistoryNode['type'],
      status: row.status as PipelineHistoryNode['status'],
      position: row.position,
      parentId: row.parentId,
      ref: row.ref,
    }));

    const edges: PipelineHistoryEdge[] = [];

    // Sequential edges: same parentId, adjacent positions
    const groupedByParent: Map<string | null, typeof rows> = new Map();
    for (const row of rows) {
      const key: string | null = row.parentId;
      if (!groupedByParent.has(key)) {
        groupedByParent.set(key, []);
      }
      groupedByParent.get(key)!.push(row);
    }

    for (const group of groupedByParent.values()) {
      for (let i = 0; i < group.length - 1; i++) {
        const fromNode: typeof rows[0] = group[i];
        const toNode: typeof rows[0] = group[i + 1];
        edges.push({ from: fromNode.id, to: toNode.id });
      }
    }

    // Parent-child edges
    for (const row of rows) {
      if (row.parentId) {
        const parentNode: typeof rows[0] | undefined = rows.find(
          (r: typeof rows[0]) => r.id === row.parentId,
        );
        if (parentNode) {
          edges.push({ from: parentNode.id, to: row.id });
        }
      }
    }

    // Ref history
    const refHistory: RefHistory[] = rows
      .filter((row: typeof rows[0]) => row.ref !== null)
      .map((row: typeof rows[0]) => ({
        ref: row.ref!,
        nodeId: row.id,
        timestamp: row.createdAt.toISOString(),
      }));

    return { nodes, edges, refHistory };
  }
}
