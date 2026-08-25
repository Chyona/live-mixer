import type { BaseResponse } from './types';
import { request } from './http';

export interface AiPrompt {
  id: number;
  name: string;
  content: string;
  remark: string;
  created_by: string;
  created_at: string;
  updated_at: string;
  is_editable: number;
}

export type AiPromptId = number;

type AiPromptRaw = Partial<AiPrompt> & {
  content_preview?: string;
};

export function normalizeAiPrompt(raw: AiPromptRaw): AiPrompt {
  const content = String(raw.content_preview ?? raw.content ?? '').trim();

  return {
    id: Number(raw.id),
    name: String(raw.name ?? ''),
    content,
    remark: String(raw.remark ?? ''),
    created_by: String(raw.created_by ?? ''),
    created_at: String(raw.created_at ?? ''),
    updated_at: String(raw.updated_at ?? ''),
    is_editable: Number(raw.is_editable ?? 0),
  };
}

export function getAiPromptContent(prompt: AiPrompt | null | undefined): string {
  return prompt?.content?.trim() ?? '';
}

function normalizeAiPromptResponse<T extends AiPrompt | null>(data: T): T {
  return (data ? normalizeAiPrompt(data) : data) as T;
}

export interface AiPromptListParams {
  keywords?: string;
  start_date?: string;
  end_date?: string;
  page?: number;
  page_size?: number;
}

export interface AiPromptListResult {
  list: AiPrompt[];
  total: number;
}

export interface CreateAiPromptParams {
  name: string;
  content: string;
  remark?: string;
}

export interface UpdateAiPromptParams {
  name: string;
  content: string;
  remark?: string;
}

export async function fetchAiPromptList(
  params: AiPromptListParams
): Promise<BaseResponse<AiPromptListResult>> {
  const response = await request<BaseResponse<AiPromptListResult>>('/v1/llm-system-prompts', {
    method: 'get',
    params,
  });

  if (response.code === 0 && response.data?.list) {
    return {
      ...response,
      data: {
        ...response.data,
        list: response.data.list.map((item) => normalizeAiPrompt(item)),
      },
    };
  }

  return response;
}

export async function createAiPrompt(
  params: CreateAiPromptParams
): Promise<BaseResponse<AiPrompt>> {
  const response = await request<BaseResponse<AiPrompt>>('/v1/llm-system-prompts', {
    method: 'post',
    data: params,
  });

  if (response.code === 0 && response.data) {
    return { ...response, data: normalizeAiPromptResponse(response.data) };
  }

  return response;
}

export async function updateAiPrompt(
  id: AiPromptId,
  params: UpdateAiPromptParams
): Promise<BaseResponse<AiPrompt>> {
  const response = await request<BaseResponse<AiPrompt>>(`/v1/llm-system-prompts/${id}`, {
    method: 'put',
    data: params,
  });

  if (response.code === 0 && response.data) {
    return { ...response, data: normalizeAiPromptResponse(response.data) };
  }

  return response;
}

export async function deleteAiPrompt(id: AiPromptId): Promise<BaseResponse<null>> {
  return await request(`/v1/llm-system-prompts/${id}`, {
    method: 'delete',
  });
}
