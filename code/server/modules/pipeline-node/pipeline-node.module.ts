import { Module } from '@nestjs/common';
import { PipelineNodeService } from './pipeline-node.service';
import { PipelineNodeController, RunPipelineController } from './pipeline-node.controller';

@Module({
  providers: [PipelineNodeService],
  controllers: [PipelineNodeController, RunPipelineController],
  exports: [PipelineNodeService],
})
export class PipelineNodeModule {}
