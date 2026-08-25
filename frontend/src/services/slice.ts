import type { BaseResponse } from './types';
import { request } from './http';

export type SliceAuditStatus = 'approved' | 'rejected' | 'pending';

export interface VideoSliceItem {
  id: string;
  name: string;
  startTime: number;
  endTime: number;
  duration: number;
  date: string;
  sourceVideoId: string;
  sourceVideoName: string;
  sourceVideoRemarkName: string;
  liveUrl: string;
  previewUrl: string;
  auditStatus: SliceAuditStatus;
}

export interface SliceListParams {
  date?: string;
  dateEnd?: string;
  keyword?: string;
  page?: number;
  pageSize?: number;
}

export interface SliceListResult {
  list: VideoSliceItem[];
  total: number;
}

export async function fetchSliceList(
  params: SliceListParams
): Promise<BaseResponse<SliceListResult>> {
  return await request('/v1/slices', {
    method: 'get',
    params,
  });
}

export async function updateSliceName(
  id: string,
  name: string
): Promise<BaseResponse<VideoSliceItem>> {
  return await request(`/v1/slices/${id}/name`, {
    method: 'put',
    data: { name },
  });
}

export interface ClipRange {
  start: number;
  end: number;
}

export interface SubmitClipParams {
  video_project_id: string | number;
  /** 是否生成字幕（可选） */
  enable_captions?: boolean;
}

export interface ClipSubmitResult {
  task_id?: string;
  taskId?: string;
}

/** 提交一键成片任务（依赖已保存的剪辑项目） */
export async function submitClip(
  params: SubmitClipParams
): Promise<BaseResponse<ClipSubmitResult>> {
  return await request('/v1/tasks/ai-slice-draft', {
    method: 'post',
    data: params,
  });
}

/** 人工切片提交成片（生成草稿） */
export async function submitDraft(
  params: SubmitClipParams
): Promise<BaseResponse<ClipSubmitResult>> {
  return await request('/v1/tasks/draft', {
    method: 'post',
    data: params,
  });
}
