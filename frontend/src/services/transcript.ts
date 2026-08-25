import type { BaseResponse } from './types';
import { request } from './http';
import type { ManualSliceDraft } from '~/pages/ManualVideoSlice/types';

export interface SaveManualSliceDraftParams {
  name: string;
  segments: ManualSliceDraft['segments'];
}

export async function saveManualSliceDraft(
  sourceVideoId: string,
  params: SaveManualSliceDraftParams
): Promise<BaseResponse<ManualSliceDraft>> {
  return await request(`/v1/source-videos/${sourceVideoId}/manual-slice-drafts`, {
    method: 'post',
    data: params,
  });
}

export async function fetchManualSliceDrafts(
  sourceVideoId: string
): Promise<BaseResponse<ManualSliceDraft[]>> {
  return await request(`/v1/source-videos/${sourceVideoId}/manual-slice-drafts`, {
    method: 'get',
  });
}
