import { Module } from '@nestjs/common';
import { MessageService } from './message.service';
import { MessageController, RunMessagesController } from './message.controller';

@Module({
  providers: [MessageService],
  controllers: [MessageController, RunMessagesController],
  exports: [MessageService],
})
export class MessageModule {}
