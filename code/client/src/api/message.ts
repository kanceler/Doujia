import { axiosForBackend } from '@lark-apaas/client-toolkit/utils/getAxiosForBackend';
import type {
  SendMessageRequest,
  MessageListResponse,
} from '@shared/api.interface';

export async function getMessagesByRunId(
  runId: string,
  cursor?: string,
  pageSize: number = 20,
): Promise<MessageListResponse> {
  const params: Record<string, string | number> = { pageSize };
  if (cursor) params.cursor = cursor;
  const response = await axiosForBackend({
    url: `/api/runs/${runId}/messages`,
    method: 'GET',
    params,
  });
  return response.data;
}

export async function sendMessage(
  data: SendMessageRequest,
): Promise<{ id: string }> {
  const response = await axiosForBackend({
    url: '/api/messages',
    method: 'POST',
    data,
  });
  return response.data;
}
