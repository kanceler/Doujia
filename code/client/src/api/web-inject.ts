import { axiosForBackend } from '@lark-apaas/client-toolkit/utils/getAxiosForBackend';
import type {
  PreviewResponse,
  ModifyRequest,
  ModifyResponse,
  SubmitMrRequest,
  SubmitMrResponse,
} from '@shared/api.interface';

export async function getPreview(runId: string): Promise<PreviewResponse> {
  const response = await axiosForBackend({
    url: `/api/runs/${runId}/preview`,
    method: 'GET',
  });
  return response.data;
}

export async function submitModification(
  runId: string,
  data: ModifyRequest,
): Promise<ModifyResponse> {
  const response = await axiosForBackend({
    url: `/api/runs/${runId}/inject/modify`,
    method: 'POST',
    data,
  });
  return response.data;
}

export async function submitMr(
  runId: string,
  data: SubmitMrRequest,
): Promise<SubmitMrResponse> {
  const response = await axiosForBackend({
    url: `/api/runs/${runId}/inject/submit-mr`,
    method: 'POST',
    data,
  });
  return response.data;
}

export async function exportRun(runId: string): Promise<{ downloadUrl: string }> {
  const response = await axiosForBackend({
    url: `/api/runs/${runId}/export`,
    method: 'GET',
  });
  return response.data;
}
