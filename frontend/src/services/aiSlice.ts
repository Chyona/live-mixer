import type { BaseResponse } from './types';
import type { SliceProjectClip, SliceProjectSource } from './sliceProject';
import type { TaskBatchCreateResult } from './task';
import { request } from './http';

export interface SubmitAiSliceParams {
  live_id: number;
  prompt_id: number;
  clips0: SliceProjectClip[];
  /** 默认 manual：AI 选片结果在人工切片页编辑 */
  project_source?: SliceProjectSource;
}

/** 提交 AI 选片任务（由后端创建剪辑项目并拆分任务） */
export async function submitAiSliceSelection(
  params: SubmitAiSliceParams
): Promise<BaseResponse<TaskBatchCreateResult>> {
  return await request('/v1/tasks/ai-slice', {
    method: 'post',
    data: params,
  });
}
