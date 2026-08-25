import type { BaseResponse } from './types';
import { request } from './http';
import type { SliceProjectClip, SliceProjectSource } from './sliceProject';

export interface SubmitAiSliceParams {
  live_id: number;
  prompt_id?: number;
  clips0: SliceProjectClip[];
  project_source?: SliceProjectSource;
  /** 兼容旧调用：已有项目时可不传 live_id / clips0 */
  video_project_id?: string | number;
}

export interface TaskCreateItem {
  id: string;
  type?: string;
  status?: string;
}

export interface AiSliceSelectResult {
  list?: TaskCreateItem[];
  total?: number;
  task_id?: string;
}

/** 提交 AI 选片：后端按选区创建项目并发布任务 */
export async function submitAiSliceSelection(
  params: SubmitAiSliceParams
): Promise<BaseResponse<AiSliceSelectResult>> {
  return await request('/v1/tasks/ai-slice', {
    method: 'post',
    data: params,
  });
}
