import { Controller, Get, Post, Query, Param, Body } from '@nestjs/common';
import { MessageService } from './message.service';
import type { MessageListResponse, SendMessageRequest } from '@shared/api.interface';

@Controller('api/messages')
export class MessageController {
  constructor(private readonly messageService: MessageService) {}

  @Post()
  async sendMessage(@Body() body: SendMessageRequest): Promise<{ id: string }> {
    return this.messageService.sendMessage(body.sessionId, body.content);
  }
}

@Controller('api/runs')
export class RunMessagesController {
  constructor(private readonly messageService: MessageService) {}

  @Get(':runId/messages')
  async getByRunId(
    @Param('runId') runId: string,
    @Query('cursor') cursor: string | undefined,
    @Query('pageSize') pageSize: string | undefined,
  ): Promise<MessageListResponse> {
    const size: number = pageSize ? parseInt(pageSize, 10) : 20;
    return this.messageService.getByRunId(runId, cursor, size);
  }
}
