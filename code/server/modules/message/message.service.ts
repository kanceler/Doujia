import { Injectable, Inject, Logger } from '@nestjs/common';
import { DRIZZLE_DATABASE, type PostgresJsDatabase } from '@lark-apaas/fullstack-nestjs-core';
import { eq, desc, lt, and, or, gt } from 'drizzle-orm';
import { messageTable } from '@server/database/schema';
import type { MessageItem, MessageListResponse } from '@shared/api.interface';

@Injectable()
export class MessageService {
  private readonly logger = new Logger(MessageService.name);

  constructor(@Inject(DRIZZLE_DATABASE) private readonly db: PostgresJsDatabase) {}

  async getByRunId(
    runId: string,
    cursor?: string,
    pageSize: number = 20,
  ): Promise<MessageListResponse> {
    const limit: number = pageSize + 1;

    let cursorDate: Date | undefined;
    if (cursor) {
      const cursorRecord: { createdAt: Date } | undefined = await this.db
        .select({ createdAt: messageTable.createdAt })
        .from(messageTable)
        .where(eq(messageTable.id, cursor))
        .limit(1)
        .then((rows: { createdAt: Date }[]) => rows[0]);
      if (cursorRecord) {
        cursorDate = cursorRecord.createdAt;
      }
    }

    const conditions = [eq(messageTable.runId, runId)];
    if (cursorDate) {
      conditions.push(
        or(
          lt(messageTable.createdAt, cursorDate),
          and(
            eq(messageTable.createdAt, cursorDate),
            lt(messageTable.id, cursor),
          ),
        ),
      );
    }

    const query = conditions.length > 1
      ? this.db
          .select()
          .from(messageTable)
          .where(and(...conditions))
          .orderBy(desc(messageTable.createdAt), desc(messageTable.id))
          .limit(limit)
      : this.db
          .select()
          .from(messageTable)
          .where(eq(messageTable.runId, runId))
          .orderBy(desc(messageTable.createdAt), desc(messageTable.id))
          .limit(limit);

    const rows: typeof messageTable.$inferSelect[] = await query;

    const hasMore: boolean = rows.length > pageSize;
    if (hasMore) {
      rows.pop();
    }

    const items: MessageItem[] = rows.map((row: typeof messageTable.$inferSelect) => ({
      id: row.id,
      runId: row.runId,
      role: row.role as MessageItem['role'],
      content: row.content,
      type: row.type as MessageItem['type'],
      metadata: (row.metadata as MessageItem['metadata']) ?? null,
      createdAt: row.createdAt.toISOString(),
    }));

    const nextCursor: string | null = hasMore && items.length > 0 ? items[items.length - 1].id : null;

    return { items, nextCursor, hasMore };
  }

  async sendMessage(sessionId: string, content: string): Promise<{ id: string }> {
    this.logger.log(`Sending user message for session: ${sessionId}`);

    const result: { id: string }[] = await this.db
      .insert(messageTable)
      .values({
        runId: sessionId,
        role: 'user',
        content,
        type: 'text',
      })
      .returning({ id: messageTable.id });

    return { id: result[0].id };
  }

  async createSystemMessage(
    runId: string,
    content: string,
    type: string,
    metadata?: object,
  ): Promise<{ id: string }> {
    this.logger.log(`Creating system message for run: ${runId}, type: ${type}`);

    const result: { id: string }[] = await this.db
      .insert(messageTable)
      .values({
        runId,
        role: 'system',
        content,
        type,
        metadata: metadata ?? null,
      })
      .returning({ id: messageTable.id });

    return { id: result[0].id };
  }
}
