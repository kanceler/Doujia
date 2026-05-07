import { Module } from '@nestjs/common';
import { RunService } from './run.service';
import { RunController, DemoRunController } from './run.controller';

@Module({
  providers: [RunService],
  controllers: [RunController, DemoRunController],
  exports: [RunService],
})
export class RunModule {}
