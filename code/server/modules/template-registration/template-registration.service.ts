import { Injectable, Inject, Logger } from '@nestjs/common';
import { DRIZZLE_DATABASE, type PostgresJsDatabase } from '@lark-apaas/fullstack-nestjs-core';
import { eq } from 'drizzle-orm';
import { pipelineTemplateTable, registrationStateTable } from '@server/database/schema';
import type {
  RegistrationStateItem,
  PipelineTemplateItem,
  PipelineTemplateDsl,
  SaveConfigRequest,
} from '@shared/api.interface';

@Injectable()
export class TemplateRegistrationService {
  private readonly logger = new Logger(TemplateRegistrationService.name);

  constructor(@Inject(DRIZZLE_DATABASE) private readonly db: PostgresJsDatabase) {}

  async getRegistrationStates(type?: string): Promise<{ items: RegistrationStateItem[] }> {
    const query = type
      ? this.db
          .select({
            id: registrationStateTable.id,
            type: registrationStateTable.type,
            name: registrationStateTable.name,
            status: registrationStateTable.status,
            errorMessage: registrationStateTable.errorMessage,
          })
          .from(registrationStateTable)
          .where(eq(registrationStateTable.type, type))
      : this.db
          .select({
            id: registrationStateTable.id,
            type: registrationStateTable.type,
            name: registrationStateTable.name,
            status: registrationStateTable.status,
            errorMessage: registrationStateTable.errorMessage,
          })
          .from(registrationStateTable);

    const rawItems: Array<{
      id: string;
      type: string;
      name: string;
      status: string;
      errorMessage: string | null;
    }> = await query;

    const items: RegistrationStateItem[] = rawItems.map((item: {
      id: string;
      type: string;
      name: string;
      status: string;
      errorMessage: string | null;
    }): RegistrationStateItem => ({
      id: item.id,
      type: item.type as RegistrationStateItem['type'],
      name: item.name,
      status: item.status as RegistrationStateItem['status'],
      errorMessage: item.errorMessage,
    }));

    this.logger.log(`Fetched ${items.length} registration states${type ? ` for type=${type}` : ''}`);
    return { items };
  }

  async getTemplates(): Promise<{ items: PipelineTemplateItem[] }> {
    const items: PipelineTemplateItem[] = await this.db
      .select({
        id: pipelineTemplateTable.id,
        name: pipelineTemplateTable.name,
        description: pipelineTemplateTable.description,
        isDefault: pipelineTemplateTable.isDefault,
      })
      .from(pipelineTemplateTable);

    this.logger.log(`Fetched ${items.length} pipeline templates`);
    return { items };
  }

  async getTemplateDsl(templateId: string): Promise<PipelineTemplateDsl | null> {
    const rows: Array<{ dsl: unknown; description: string | null }> = await this.db
      .select({
        dsl: pipelineTemplateTable.dsl,
        description: pipelineTemplateTable.description,
      })
      .from(pipelineTemplateTable)
      .where(eq(pipelineTemplateTable.id, templateId));

    if (rows.length === 0) {
      this.logger.log(`Template not found: templateId=${templateId}`);
      return null;
    }

    const row: { dsl: unknown; description: string | null } = rows[0];
    return {
      dsl: row.dsl as PipelineTemplateDsl['dsl'],
      description: row.description ?? '',
    };
  }

  async saveConfig(_body: SaveConfigRequest): Promise<{ success: boolean }> {
    this.logger.log('Config saved (in-memory mode)');
    return { success: true };
  }
}
