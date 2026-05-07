import { Injectable, Inject, Logger } from '@nestjs/common';
import { DRIZZLE_DATABASE, type PostgresJsDatabase } from '@lark-apaas/fullstack-nestjs-core';
import type { PreviewResponse, ModifyResponse, SubmitMrResponse } from '@shared/api.interface';

@Injectable()
export class WebInjectService {
  private readonly logger = new Logger(WebInjectService.name);

  constructor(@Inject(DRIZZLE_DATABASE) private readonly db: PostgresJsDatabase) {}

  async getPreview(runId: string): Promise<PreviewResponse> {
    this.logger.log(`Getting preview for run: ${runId}`);
    return {
      html: `<div>Preview for run ${runId}</div>`,
      baseUrl: 'http://localhost:3000',
    };
  }

  async submitModification(
    runId: string,
    elementSelector: string,
    elementContent: string,
    modifyInstruction: string,
  ): Promise<ModifyResponse> {
    this.logger.log(
      `Submitting modification for run: ${runId}, selector: ${elementSelector}`,
    );
    return {
      success: true,
      modifiedHtml: '<div>Modified content</div>',
      diff: `Modified ${elementSelector}`,
    };
  }

  async submitMr(
    runId: string,
    mrTitle: string,
    mrDescription: string,
    diff: string,
  ): Promise<SubmitMrResponse> {
    this.logger.log(`Submitting MR for run: ${runId}, title: ${mrTitle}`);
    return {
      success: true,
      mrUrl: `https://git.example.com/merge-requests/${runId}`,
    };
  }

  async exportRun(runId: string): Promise<{ downloadUrl: string }> {
    this.logger.log(`Exporting run: ${runId}`);
    return {
      downloadUrl: `/api/runs/${runId}/download`,
    };
  }
}
