import { axiosForBackend } from '@lark-apaas/client-toolkit/utils/getAxiosForBackend';
import type {
  DemoRunItem,
  RunItem,
  CreateRunRequest,
  ValidateConfigRequest,
  ValidateConfigResponse,
  ApproveRequest,
  RetryRequest,
  CloneRunRequest,
} from '@shared/api.interface';

export async function getDemoRuns(): Promise<{ items: DemoRunItem[] }> {
  const response = await axiosForBackend({ url: '/api/demo-runs', method: 'GET' });
  return response.data;
}

export async function getRunById(runId: string): Promise<RunItem> {
  const response = await axiosForBackend({ url: `/api/runs/${runId}`, method: 'GET' });
  return response.data;
}

export async function createRun(data: CreateRunRequest): Promise<{ id: string }> {
  const response = await axiosForBackend({ url: '/api/runs', method: 'POST', data });
  return response.data;
}

export async function validateConfig(
  data: ValidateConfigRequest,
): Promise<ValidateConfigResponse> {
  const response = await axiosForBackend({
    url: '/api/runs/validate-config',
    method: 'POST',
    data,
  });
  return response.data;
}

export async function approveRun(
  runId: string,
  data: ApproveRequest,
): Promise<{ success: boolean }> {
  const response = await axiosForBackend({
    url: `/api/runs/${runId}/approve`,
    method: 'POST',
    data,
  });
  return response.data;
}

export async function retryRun(
  runId: string,
  data: RetryRequest,
): Promise<{ success: boolean }> {
  const response = await axiosForBackend({
    url: `/api/runs/${runId}/retry`,
    method: 'POST',
    data,
  });
  return response.data;
}

export async function cloneRun(
  runId: string,
  data: CloneRunRequest,
): Promise<{ newRunId: string }> {
  const response = await axiosForBackend({
    url: `/api/runs/${runId}/clone`,
    method: 'POST',
    data,
  });
  return response.data;
}
