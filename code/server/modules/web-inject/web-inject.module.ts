import { Module } from '@nestjs/common';
import { WebInjectController } from './web-inject.controller';
import { WebInjectService } from './web-inject.service';

@Module({
  controllers: [WebInjectController],
  providers: [WebInjectService],
})
export class WebInjectModule {}
