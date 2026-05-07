import { Controller, Get, Param, NotFoundException } from '@nestjs/common';
import { PipelineNodeService } from './pipeline-node.service';
import type {
  PipelineNodeItem,
  PipelineNodeDetail,
  PipelineHistoryResponse,
} from '@shared/api.interface';

@Controller('api/pipeline-nodes')
export class PipelineNodeController {
  constructor(private readonly pipelineNodeService: PipelineNodeService) {}

  @Get('/:nodeId')
  async getNodeDetail(
    @Param('nodeId') nodeId: string,
  ): Promise<PipelineNodeDetail> {
    const detail: PipelineNodeDetail | null = await this.pipelineNodeService.getNodeDetail(nodeId);
    if (!detail) {
      throw new NotFoundException(`Pipeline node not found: ${nodeId}`);
    }
    return detail;
  }
}

@Controller('api/runs')
export class RunPipelineController {
  constructor(private readonly pipelineNodeService: PipelineNodeService) {}

  @Get('/:runId/pipeline-nodes')
  async getByRunId(
    @Param('runId') runId: string,
  ): Promise<{ items: PipelineNodeItem[] }> {
    return this.pipelineNodeService.getByRunId(runId);
  }

  @Get('/:runId/pipeline-history')
  async getPipelineHistory(
    @Param('runId') runId: string,
  ): Promise<PipelineHistoryResponse> {
    return this.pipelineNodeService.getPipelineHistory(runId);
  }
}
