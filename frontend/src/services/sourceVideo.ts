import axios from 'axios';

import { apiPath } from '~/utils/api';

import type { BaseResponse } from './types';
import { AppError, DEFAULT_REQUEST_TIMEOUT_MS, request } from './http';

export type {
  AsrStatus,
  SourceVideo,
  SourceVideoAsrFields,
} from './sourceVideo.model';
export {
  createInitialAsrState,
  isSourceVideoUrlDuplicateError,
  SOURCE_VIDEO_URL_DUPLICATE_CODE,
} from './sourceVideo.model';

import type { SourceVideo } from './sourceVideo.model';

function parseContentDispositionFilename(header?: string): string | null {
  if (!header) return null;

  const utf8Match = /filename\*=UTF-8''([^;]+)/i.exec(header);
  if (utf8Match?.[1]) {
    try {
      return decodeURIComponent(utf8Match[1].trim());
    } catch {
      return utf8Match[1].trim();
    }
  }

  const plainMatch = /filename="?([^";]+)"?/i.exec(header);
  return plainMatch?.[1]?.trim() || null;
}

function triggerBlobDownload(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
  URL.revokeObjectURL(url);
}

async function resolveBlobErrorMessage(error: unknown, fallback: string): Promise<string> {
  if (!(error instanceof AppError)) {
    return fallback;
  }

  const data = error.resp?.response?.data;
  if (data instanceof Blob) {
    try {
      const text = await data.text();
      const payload = JSON.parse(text) as { message?: string; errorMessage?: string };
      const message =
        (typeof payload.message === 'string' && payload.message.trim()) ||
        (typeof payload.errorMessage === 'string' && payload.errorMessage.trim()) ||
        '';
      if (message) return message;
    } catch {
      // ignore parse failure
    }
  }

  return error.errorMessage || fallback;
}

export interface SourceVideoListParams {
  /** 开始日期 YYYY-MM-DD */
  start_date?: string;
  /** 结束日期 YYYY-MM-DD */
  end_date?: string;
  /** 关键词，英文逗号分隔，匹配 name/remark */
  keywords?: string;
  /** ASR 文案关键词，英文逗号分隔，匹配已解析的视频文案 */
  asr_keywords?: string;
  page?: number;
  /** 每页数量，默认 10 */
  page_size?: number;
}

export interface SourceVideoListResult {
  list: SourceVideo[];
  total: number;
}

export interface CreateSourceVideoParams {
  name: string;
  live_url: string;
  remark?: string;
}

export type SourceVideoId = number | string;

function normalizeProjectCount(raw: Partial<SourceVideo> & Record<string, unknown>): number {
  const value = Number(
    raw.project_count ?? raw.video_project_count ?? raw.projectCount ?? 0
  );
  return Number.isFinite(value) && value > 0 ? Math.floor(value) : 0;
}

function normalizeAsrSummaries(raw: unknown): SourceVideo['asr_summaries'] {
  if (!Array.isArray(raw)) return null;
  const list = raw
    .map((item) => {
      if (!item || typeof item !== 'object') return null;
      const row = item as Record<string, unknown>;
      const start = Number(row.start_time);
      const end = Number(row.end_time);
      if (!Number.isFinite(start) || !Number.isFinite(end) || end <= start) return null;
      return {
        title: String(row.title ?? '').trim(),
        start_time: start,
        end_time: end,
      };
    })
    .filter((item): item is NonNullable<typeof item> => item != null);
  return list.length ? list : [];
}

function normalizeMatchedParagraphs(raw: unknown): SourceVideo['matched_paragraphs'] {
  if (!Array.isArray(raw)) return null;
  const list = raw
    .map((item) => {
      if (!item || typeof item !== 'object') return null;
      const row = item as Record<string, unknown>;
      const text = String(row.text ?? '').trim();
      if (!text) return null;
      const start = Number(row.start_time);
      const end = Number(row.end_time);
      return {
        speaker: String(row.speaker ?? ''),
        start_time: Number.isFinite(start) ? start : 0,
        end_time: Number.isFinite(end) ? end : 0,
        text,
      };
    })
    .filter((item): item is NonNullable<typeof item> => item != null);
  return list.length ? list : [];
}

export function normalizeSourceVideo(
  raw: Partial<SourceVideo> & Record<string, unknown>
): SourceVideo {
  return {
    id: Number(raw.id ?? 0),
    name: String(raw.name ?? ''),
    live_url: String(raw.live_url ?? ''),
    remark: String(raw.remark ?? ''),
    duration: Number(raw.duration ?? 0),
    ext: String(raw.ext ?? ''),
    asr_status: (raw.asr_status as SourceVideo['asr_status']) || 'pending',
    asr_progress: Number(raw.asr_progress ?? 0),
    asr_error_msg: String(raw.asr_error_msg ?? ''),
    asr_started_at: String(raw.asr_started_at ?? ''),
    asr_updated_at: String(raw.asr_updated_at ?? ''),
    created_at: String(raw.created_at ?? ''),
    updated_at: String(raw.updated_at ?? ''),
    created_by: String(raw.created_by ?? ''),
    project_count: normalizeProjectCount(raw),
    matched_paragraphs: normalizeMatchedParagraphs(
      raw.matched_paragraphs ?? raw.matchedParagraphs
    ),
    asr_paragraphs: (raw.asr_paragraphs as SourceVideo['asr_paragraphs']) ?? null,
    asr_summaries: normalizeAsrSummaries(raw.asr_summaries),
  };
}

export async function fetchSourceVideoList(
  params: SourceVideoListParams
): Promise<BaseResponse<SourceVideoListResult>> {
  const response = await request<BaseResponse<SourceVideoListResult>>('/v1/live-materials', {
    method: 'get',
    params,
  });

  return {
    ...response,
    data: {
      list: (response.data?.list ?? []).map((item) =>
        normalizeSourceVideo(item as Partial<SourceVideo> & Record<string, unknown>)
      ),
      total: Number(response.data?.total ?? 0),
    },
  };
}

export async function fetchSourceVideoDetail(
  id: SourceVideoId
): Promise<BaseResponse<SourceVideo>> {
  const response = await request<BaseResponse<SourceVideo>>(`/v1/live-materials/${id}`, {
    method: 'get',
  });

  return {
    ...response,
    data: normalizeSourceVideo(
      (response.data ?? {}) as Partial<SourceVideo> & Record<string, unknown>
    ),
  };
}

/** 修改源视频名称或备注：接口要求 name、remark 两个字段都传 */
export async function updateSourceVideo(
  id: SourceVideoId,
  params: Partial<CreateSourceVideoParams>
): Promise<BaseResponse<SourceVideo>> {
  const response = await request<BaseResponse<SourceVideo>>(`/v1/live-materials/${id}`, {
    method: 'put',
    data: params,
  });
  return {
    ...response,
    data: normalizeSourceVideo(
      (response.data ?? {}) as Partial<SourceVideo> & Record<string, unknown>
    ),
  };
}

export async function deleteSourceVideo(id: SourceVideoId): Promise<BaseResponse<null>> {
  return await request(`/v1/live-materials/${id}`, {
    method: 'delete',
  });
}

export async function createSourceVideo(
  params: CreateSourceVideoParams
): Promise<BaseResponse<SourceVideo>> {
  const response = await request<BaseResponse<SourceVideo>>('/v1/live-materials', {
    method: 'post',
    data: params,
  });
  return {
    ...response,
    data: normalizeSourceVideo(
      (response.data ?? {}) as Partial<SourceVideo> & Record<string, unknown>
    ),
  };
}

export async function retrySourceVideoAsr(id: SourceVideoId): Promise<BaseResponse<SourceVideo>> {
  const response = await request<BaseResponse<SourceVideo>>(`/v1/live-materials/${id}/asr/retry`, {
    method: 'post',
  });
  return {
    ...response,
    data: normalizeSourceVideo(
      (response.data ?? {}) as Partial<SourceVideo> & Record<string, unknown>
    ),
  };
}

/**
 * 下载 ASR 字幕（原始 asr JSON）。
 * 仅 asr_status=completed 且内容非空时可下载。
 */
export async function downloadSourceVideoAsrSubtitle(
  id: SourceVideoId,
  fallbackFilename: string
): Promise<void> {
  try {
    const response = await axios.request<Blob>({
      url: apiPath(`/v1/live-materials/${id}/asr/subtitle`),
      method: 'get',
      responseType: 'blob',
      timeout: DEFAULT_REQUEST_TIMEOUT_MS,
    });

    const blob = response.data;
    if (!blob || blob.size === 0) {
      throw new Error('暂无字幕文案');
    }

    const filename =
      parseContentDispositionFilename(response.headers['content-disposition']) || fallbackFilename;

    triggerBlobDownload(blob, filename);
  } catch (error) {
    if (error instanceof Error && error.message === '暂无字幕文案') {
      throw error;
    }

    const message = await resolveBlobErrorMessage(error, '字幕下载失败');
    if (error instanceof AppError) {
      throw new AppError(message, error.errorCode, error.resp);
    }
    throw new Error(message);
  }
}
