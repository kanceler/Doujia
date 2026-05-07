import {
  Controller,
  Get,
  Post,
  Body,
  Param,
  Req,
} from '@nestjs/common';
import { NeedLogin } from '@lark-apaas/fullstack-nestjs-core';
import type { Request } from 'express';
import type {
  PreviewResponse,
  ModifyResponse,
  SubmitMrResponse,
  ModifyRequest,
  SubmitMrRequest,
} from '@shared/api.interface';
import { WebInjectService } from './web-inject.service';

@Controller('api/runs')
export class WebInjectController {
  constructor(private readonly webInjectService: WebInjectService) {}

  @Get(':runId/preview')
  async getPreview(
    @Param('runId') runId: string,
  ): Promise<PreviewResponse> {
    return this.webInjectService.getPreview(runId);
  }

  @Post(':runId/inject/modify')
  @NeedLogin()
  async submitModification(
    @Param('runId') runId: string,
    @Body() body: ModifyRequest,
  ): Promise<ModifyResponse> {
    return this.webInjectService.submitModification(
      runId,
      body.elementSelector,
      body.elementContent,
      body.modifyInstruction,
    );
  }

  @Post(':runId/inject/submit-mr')
  @NeedLogin()
  async submitMr(
    @Param('runId') runId: string,
    @Body() body: SubmitMrRequest,
  ): Promise<SubmitMrResponse> {
    return this.webInjectService.submitMr(
      runId,
      body.mrTitle,
      body.mrDescription,
      body.diff,
    );
  }

  @Get(':runId/export')
  async exportRun(
    @Param('runId') runId: string,
  ): Promise<{ downloadUrl: string }> {
    return this.webInjectService.exportRun(runId);
  }
}
