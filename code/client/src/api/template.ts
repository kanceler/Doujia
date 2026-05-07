import { axiosForBackend } from '@lark-apaas/client-toolkit/utils/getAxiosForBackend';
import type {
  RegistrationStateItem,
  PipelineTemplateItem,
  PipelineTemplateDsl,
  SaveConfigRequest,
} from '@shared/api.interface';

export async function getRegistrationStates(
  type?: string,
): Promise<{ items: RegistrationStateItem[] }> {
  const params: Record<string, string> = {};
  if (type) params.type = type;
  const response = await axiosForBackend({
    url: '/api/registration-state',
    method: 'GET',
    params,
  });
  return response.data;
}

export async function getPipelineTemplates(): Promise<{
  items: PipelineTemplateItem[];
}> {
  const response = await axiosForBackend({
    url: '/api/pipeline-templates',
    method: 'GET',
  });
  return response.data;
}

export async function getPipelineTemplateDsl(
  templateId: string,
): Promise<PipelineTemplateDsl> {
  const response = await axiosForBackend({
    url: `/api/pipeline-templates/${templateId}/dsl`,
    method: 'GET',
  });
  return response.data;
}

export async function saveConfig(
  data: SaveConfigRequest,
): Promise<{ success: boolean }> {
  const response = await axiosForBackend({
    url: '/api/config',
    method: 'POST',
    data,
  });
  return response.data;
}
