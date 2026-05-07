import { Injectable, Inject } from '@nestjs/common';
import { Logger } from '@nestjs/common';
import { eq } from 'drizzle-orm';
import { DRIZZLE_DATABASE, type PostgresJsDatabase } from '@lark-apaas/fullstack-nestjs-core';
import { runTable } from '@server/database/schema';
import type {
  RunConfig,
  ValidateConfigResponse,
  DemoRunItem,
  RunItem,
  CreateRunRequest,
} from '@shared/api.interface';

@Injectable()
export class RunService {
  private readonly logger = new Logger(RunService.name);

  constructor(
    @Inject(DRIZZLE_DATABASE) private readonly db: PostgresJsDatabase,
  ) {}

  async getDemoRuns(): Promise<{ items: DemoRunItem[] }> {
    this.logger.log('Fetching demo runs');
    const rows = await this.db
      .select({
        id: runTable.id,
        name: runTable.name,
        demandSummary: runTable.demandSummary,
        status: runTable.status,
        createdAt: runTable.createdAt,
      })
      .from(runTable)
      .where(eq(runTable.isDemo, true));

    const items: DemoRunItem[] = rows.map((row: {
      id: string;
      name: string;
      demandSummary: string | null;
      status: string;
      createdAt: Date;
    }) => ({
      id: row.id,
      name: row.name,
      description: row.demandSummary ?? '',
      status: row.status as DemoRunItem['status'],
      createdAt: row.createdAt.toISOString(),
    }));

    return { items };
  }

  async getById(runId: string): Promise<RunItem | null> {
    this.logger.log(`Fetching run by id: ${runId}`);
    const rows = await this.db
      .select()
      .from(runTable)
      .where(eq(runTable.id, runId));

    if (rows.length === 0) {
      return null;
    }

    const row = rows[0];
    return {
      id: row.id,
      name: row.name,
      status: row.status as RunItem['status'],
      demandSummary: row.demandSummary,
      config: row.config as RunConfig | null,
      ref: row.ref,
      isDemo: row.isDemo,
      createdAt: row.createdAt.toISOString(),
      updatedAt: row.updatedAt.toISOString(),
    };
  }

  async create(data: CreateRunRequest): Promise<{ id: string }> {
    this.logger.log(`Creating run: ${JSON.stringify(data)}`);
    const name = data.demandSummary.slice(0, 50);
    const rows = await this.db
      .insert(runTable)
      .values({
        name,
        status: 'pending',
        demandSummary: data.demandSummary,
        config: data.config as unknown as Record<string, unknown>,
        ref: 'ref-init',
        isDemo: false,
      })
      .returning({ id: runTable.id });

    const createdId: string = rows[0].id;
    this.logger.log(`Run created with id: ${createdId}`);
    return { id: createdId };
  }

  validateConfig(config: RunConfig): ValidateConfigResponse {
    this.logger.log(`Validating config: ${JSON.stringify(config)}`);
    const errors: string[] = [];

    if (!config.modelProvider) {
      errors.push('modelProvider is required');
    }
    if (!config.modelName) {
      errors.push('modelName is required');
    }
    if (!config.templateId) {
      errors.push('templateId is required');
    }

    return { valid: errors.length === 0, errors };
  }

  async approve(
    runId: string,
    nodeId: string,
    comment?: string,
  ): Promise<{ success: boolean }> {
    this.logger.log(
      `Approving run ${runId} node ${nodeId}, comment: ${comment ?? 'none'}`,
    );
    return { success: true };
  }

  async retry(
    runId: string,
    nodeId: string,
    action: string,
    comment?: string,
  ): Promise<{ success: boolean }> {
    this.logger.log(
      `Retrying run ${runId} node ${nodeId} with action ${action}, comment: ${comment ?? 'none'}`,
    );
    return { success: true };
  }

  async clone(
    runId: string,
    newName: string,
    configOverrides?: Partial<RunConfig>,
  ): Promise<{ newRunId: string }> {
    this.logger.log(`Cloning run ${runId} with new name: ${newName}`);
    const sourceRows = await this.db
      .select()
      .from(runTable)
      .where(eq(runTable.id, runId));

    if (sourceRows.length === 0) {
      throw new Error(`Run not found: ${runId}`);
    }

    const source = sourceRows[0];
    let mergedConfig: Record<string, unknown> = (source.config as Record<string, unknown>) ?? {};
    if (configOverrides) {
      mergedConfig = { ...mergedConfig, ...configOverrides };
    }

    const rows = await this.db
      .insert(runTable)
      .values({
        name: newName,
        status: 'pending',
        demandSummary: source.demandSummary,
        config: mergedConfig,
        ref: 'ref-init',
        isDemo: false,
      })
      .returning({ id: runTable.id });

    const newRunId: string = rows[0].id;
    this.logger.log(`Run cloned with new id: ${newRunId}`);
    return { newRunId };
  }
}
