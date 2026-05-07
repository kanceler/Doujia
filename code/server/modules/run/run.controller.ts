import {
  Controller,
  Get,
  Post,
  Body,
  Param,
  NotFoundException,
} from '@nestjs/common';
import { NeedLogin } from '@lark-apaas/fullstack-nestjs-core';
import { RunService } from './run.service';
import type {
  DemoRunItem,
  RunItem,
  CreateRunRequest,
  ValidateConfigRequest,
  ValidateConfigResponse,
  ApproveRequest,
  RetryRequest,
  CloneRunRequest,
} from '@shared/api.interface';

@Controller('api/runs')
export class RunController {
  constructor(private readonly runService: RunService) {}

  @Get('/:runId')
  async getById(
    @Param('runId') runId: string,
  ): Promise<RunItem> {
    const run = await this.runService.getById(runId);
    if (!run) {
      throw new NotFoundException(`Run not found: ${runId}`);
    }
    return run;
  }

  @Post()
  @NeedLogin()
  async create(
    @Body() body: CreateRunRequest,
  ): Promise<{ id: string }> {
    return this.runService.create(body);
  }

  @Post('/validate-config')
  async validateConfig(
    @Body() body: ValidateConfigRequest,
  ): Promise<ValidateConfigResponse> {
    return this.runService.validateConfig(body.config);
  }

  @Post('/:runId/approve')
  @NeedLogin()
  async approve(
    @Param('runId') runId: string,
    @Body() body: ApproveRequest,
  ): Promise<{ success: boolean }> {
    return this.runService.approve(runId, body.nodeId, body.comment);
  }

  @Post('/:runId/retry')
  @NeedLogin()
  async retry(
    @Param('runId') runId: string,
    @Body() body: RetryRequest,
  ): Promise<{ success: boolean }> {
    return this.runService.retry(runId, body.nodeId, body.action, body.comment);
  }

  @Post('/:runId/clone')
  @NeedLogin()
  async clone(
    @Param('runId') runId: string,
    @Body() body: CloneRunRequest,
  ): Promise<{ newRunId: string }> {
    return this.runService.clone(runId, body.newName, body.configOverrides);
  }
}

@Controller('api/demo-runs')
export class DemoRunController {
  constructor(private readonly runService: RunService) {}

  @Get()
  async getDemoRuns(): Promise<{ items: DemoRunItem[] }> {
    return this.runService.getDemoRuns();
  }
}
