import { axiosForBackend } from '@lark-apaas/client-toolkit/utils/getAxiosForBackend';
import type {
  PipelineNodeItem,
  PipelineNodeDetail,
  PipelineHistoryResponse,
} from '@shared/api.interface';

export async function getPipelineNodesByRunId(
  runId: string,
): Promise<{ items: PipelineNodeItem[] }> {
  const response = await axiosForBackend({
    url: `/api/runs/${runId}/pipeline-nodes`,
    method: 'GET',
  });
  return response.data;
}

export async function getPipelineNodeDetail(
  nodeId: string,
): Promise<PipelineNodeDetail> {
  const response = await axiosForBackend({
    url: `/api/pipeline-nodes/${nodeId}`,
    method: 'GET',
  });
  return response.data;
}

export async function getPipelineHistory(
  runId: string,
): Promise<PipelineHistoryResponse> {
  const response = await axiosForBackend({
    url: `/api/runs/${runId}/pipeline-history`,
    method: 'GET',
  });
  return response.data;
}
