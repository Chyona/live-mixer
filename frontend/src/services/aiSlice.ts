import type { BaseResponse } from './types';
import { request } from './http';

export interface SubmitAiSliceParams {
  video_project_id: string | number;
}

export interface AiSliceSelectResult {
  task_id?: string;
}

/** 提交 AI 选片任务（依赖已保存的剪辑项目） */
export async function submitAiSliceSelection(
  params: SubmitAiSliceParams
): Promise<BaseResponse<AiSliceSelectResult>> {
  return await request('/v1/tasks/ai-slice', {
    method: 'post',
    data: params,
  });
}
