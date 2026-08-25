import axios from 'axios';

import { apiPath } from '~/utils/api';

import type { BaseResponse } from './types';
import { AppError, request } from './http';

export type ClipTaskItemStatus = 'pending' | 'processing' | 'completed' | 'failed';

/** 任务类型 */
export type GenerationTaskType = 'ai_slice' | 'draft' | 'ai_slice_draft' | (string & {});

/** 大文件下载超时（合成视频 / 片段压缩包） */
const TASK_DOWNLOAD_TIMEOUT_MS = 5 * 60 * 1000;

/** ext 字段解析结果 */
export interface ClipTaskExt {
  live_id?: number;
  live_name?: string;
  video_project_id?: number;
  sys_prompt_id?: number;
  target_duration_ms?: number;
  draft_url?: string;
  draft_urls?: string[];
  live_url?: string;
  video_url?: string;
  video_urls?: string[];
  clips_tar_url?: string;
}

/**
 * 任务列表项（与接口返回结构一致）
 */
export interface ClipTaskItem {
  id: string;
  type: GenerationTaskType;
  status: ClipTaskItemStatus;
  progress: number;
  /** 系统提示词 */
  sys_prompt: string;
  /** 用户提示词 */
  usr_prompt: string;
  /** 项目名称 */
  video_project_name: string;
  /** 源视频名称 */
  live_name: string;
  /** 直播素材 URL */
  live_url: string;
  /** 草稿地址（一键成片 / 生成草稿） */
  draft_url: string;
  /** 合成后的视频地址 */
  video_url: string;
  /** 全部视频片段压缩包地址 */
  clips_tar_url: string;
  /** 视频宽度（像素） */
  width?: number;
  /** 视频高度（像素） */
  height?: number;
  created_by: string;
  error_message: string;
  /** 原始 JSON 字符串 */
  ext: string;
  created_at: string;
  started_at: string;
  completed_at: string;
  updated_at: string;
}

export interface ClipTaskListResult {
  list: ClipTaskItem[];
  total: number;
  page?: number;
  page_size?: number;
}

/** 任务列表查询参数 */
export interface ClipTaskListParams {
  /** 任务类型：ai_slice / draft / ai_slice_draft */
  type?: GenerationTaskType;
  /** 任务状态：pending / processing / completed / failed */
  status?: ClipTaskItemStatus;
  /** 开始日期 YYYY-MM-DD，按 created_at 筛选 */
  start_date?: string;
  /** 结束日期 YYYY-MM-DD，按 created_at 筛选 */
  end_date?: string;
  /** 关键词搜索 */
  keywords?: string;
  page?: number;
  /** 每页数量，默认 10 */
  page_size?: number;
}

export function parseClipTaskExt(ext: string | ClipTaskExt | null | undefined): ClipTaskExt {
  if (!ext) return {};
  if (typeof ext === 'object') return ext;
  try {
    const parsed = JSON.parse(ext) as ClipTaskExt;
    return parsed && typeof parsed === 'object' ? parsed : {};
  } catch {
    return {};
  }
}

function resolveLiveUrl(
  raw: (Partial<ClipTaskItem> & Record<string, unknown>) | null | undefined,
  ext: ClipTaskExt
): string {
  // 不再回退到 video_url：该字段表示合成视频，与源视频 live_url 区分
  const topLevel = String(raw?.live_url ?? '').trim();
  if (topLevel) return topLevel;
  if (ext.live_url?.trim()) return ext.live_url.trim();
  return '';
}

function resolveDraftUrl(
  raw: (Partial<ClipTaskItem> & Record<string, unknown>) | null | undefined
): string {
  const topLevel = String(raw?.draft_url ?? '').trim();
  if (topLevel) return topLevel;

  const draftUrls = raw?.draft_urls;
  if (Array.isArray(draftUrls) && draftUrls.length > 0) {
    const first = String(draftUrls[0] ?? '').trim();
    if (first) return first;
  }

  const ext = parseClipTaskExt(
    typeof raw?.ext === 'string' ? raw.ext : raw?.ext != null ? JSON.stringify(raw.ext) : ''
  );
  if (ext.draft_url?.trim()) return ext.draft_url.trim();
  if (Array.isArray(ext.draft_urls) && ext.draft_urls[0]?.trim()) {
    return ext.draft_urls[0].trim();
  }

  return '';
}

function resolveVideoUrl(
  raw: (Partial<ClipTaskItem> & Record<string, unknown>) | null | undefined,
  ext: ClipTaskExt
): string {
  const topLevel = String(raw?.video_url ?? '').trim();
  if (topLevel) return topLevel;

  const videoUrls = raw?.video_urls;
  if (Array.isArray(videoUrls) && videoUrls.length > 0) {
    const first = String(videoUrls[0] ?? '').trim();
    if (first) return first;
  }

  if (ext.video_url?.trim()) return ext.video_url.trim();
  if (Array.isArray(ext.video_urls) && ext.video_urls[0]?.trim()) {
    return ext.video_urls[0].trim();
  }

  return '';
}

function resolveClipsTarUrl(
  raw: (Partial<ClipTaskItem> & Record<string, unknown>) | null | undefined,
  ext: ClipTaskExt
): string {
  const topLevel = String(
    raw?.clips_tar_url ??
      (raw as { clips_zip_url?: string })?.clips_zip_url ??
      (raw as { video_clips_tar_url?: string })?.video_clips_tar_url ??
      (raw as { segments_tar_url?: string })?.segments_tar_url ??
      ''
  ).trim();
  if (topLevel) return topLevel;
  if (ext.clips_tar_url?.trim()) return ext.clips_tar_url.trim();
  return '';
}

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
  const objectUrl = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = objectUrl;
  anchor.download = filename;
  anchor.click();
  URL.revokeObjectURL(objectUrl);
}

function triggerUrlDownload(url: string, filename: string) {
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  anchor.target = '_blank';
  anchor.rel = 'noopener noreferrer';
  document.body.appendChild(anchor);
  anchor.click();
  anchor.remove();
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
      if (payload.message) return payload.message;
      if (payload.errorMessage) return payload.errorMessage;
    } catch {
      // ignore parse failure
    }
  }

  return error.errorMessage || fallback;
}

async function downloadRemoteFile(url: string, filename: string): Promise<void> {
  try {
    const response = await axios.request<Blob>({
      url,
      method: 'get',
      responseType: 'blob',
      timeout: TASK_DOWNLOAD_TIMEOUT_MS,
    });
    const blob = response.data;
    if (!blob || blob.size === 0) {
      throw new Error('下载文件为空');
    }
    const resolvedName =
      parseContentDispositionFilename(response.headers['content-disposition']) || filename;
    triggerBlobDownload(blob, resolvedName);
  } catch {
    // 跨域或直链场景：回退为浏览器打开/下载
    triggerUrlDownload(url, filename);
  }
}

async function downloadTaskBlob(path: string, fallbackFilename: string, fallbackError: string) {
  try {
    const response = await axios.request<Blob>({
      url: apiPath(path),
      method: 'get',
      responseType: 'blob',
      timeout: TASK_DOWNLOAD_TIMEOUT_MS,
    });

    const blob = response.data;
    if (!blob || blob.size === 0) {
      throw new Error(fallbackError);
    }

    // 兼容接口返回 JSON：{ code, data: { url } }
    if (blob.type.includes('application/json') || blob.type.includes('text/')) {
      try {
        const payload = JSON.parse(await blob.text()) as {
          code?: number;
          message?: string;
          data?: { url?: string } | string | null;
        };
        if (payload.code != null && payload.code !== 0) {
          throw new Error(payload.message || fallbackError);
        }
        const url =
          typeof payload.data === 'string'
            ? payload.data.trim()
            : String(payload.data?.url ?? '').trim();
        if (!url) {
          throw new Error(fallbackError);
        }
        await downloadRemoteFile(url, fallbackFilename);
        return;
      } catch (error) {
        if (error instanceof Error) throw error;
        throw new Error(fallbackError);
      }
    }

    const filename =
      parseContentDispositionFilename(response.headers['content-disposition']) || fallbackFilename;
    triggerBlobDownload(blob, filename);
  } catch (error) {
    if (error instanceof Error && (error.message === fallbackError || error.message === '下载文件为空')) {
      throw error;
    }
    const message = await resolveBlobErrorMessage(error, fallbackError);
    if (error instanceof AppError) {
      throw new AppError(message, error.errorCode, error.resp);
    }
    throw new Error(message);
  }
}

function sanitizeDownloadFilename(name: string, fallback: string) {
  const trimmed = name.trim() || fallback;
  return trimmed.replace(/[\\/:*?"<>|]+/g, '_');
}

function resolveLiveName(
  raw: (Partial<ClipTaskItem> & Record<string, unknown>) | null | undefined,
  ext: ClipTaskExt
): string {
  const topLevel = String(
    raw?.live_name ??
      (raw as { source_video_name?: string })?.source_video_name ??
      (raw as { liveName?: string })?.liveName ??
      ''
  ).trim();
  if (topLevel) return topLevel;
  if (ext.live_name?.trim()) return ext.live_name.trim();
  return '';
}

export function normalizeClipTaskItem(raw: Partial<ClipTaskItem> | null | undefined): ClipTaskItem {
  const type = String(raw?.type ?? '');
  const normalizedType: GenerationTaskType =
    type === 'ai_slice_select' ? 'ai_slice' : type === 'clip_generate' ? 'ai_slice_draft' : type || 'draft';

  const rawStatus = String(raw?.status ?? 'pending');
  const statusMap: Record<string, ClipTaskItemStatus> = {
    pending: 'pending',
    processing: 'processing',
    running: 'processing',
    completed: 'completed',
    success: 'completed',
    failed: 'failed',
    error: 'failed',
  };

  const ext =
    typeof raw?.ext === 'string' ? raw.ext : raw?.ext != null ? JSON.stringify(raw.ext) : '';
  const parsedExt = parseClipTaskExt(ext);
  const rawRecord = raw as Partial<ClipTaskItem> & Record<string, unknown>;

  return {
    id: String(raw?.id ?? '').trim(),
    type: normalizedType,
    status: statusMap[rawStatus] ?? 'pending',
    progress: Number(raw?.progress ?? 0),
    sys_prompt: String(raw?.sys_prompt ?? ''),
    usr_prompt: String(raw?.usr_prompt ?? (raw as { usrPrompt?: string })?.usrPrompt ?? ''),
    video_project_name: String(
      (raw as { video_project_name?: string; project_name?: string })?.video_project_name ??
        (raw as { project_name?: string })?.project_name ??
        ''
    ),
    live_name: resolveLiveName(rawRecord, parsedExt),
    live_url: resolveLiveUrl(rawRecord, parsedExt),
    draft_url: resolveDraftUrl(rawRecord),
    video_url: resolveVideoUrl(rawRecord, parsedExt),
    clips_tar_url: resolveClipsTarUrl(rawRecord, parsedExt),
    width: Number(raw?.width) > 0 ? Number(raw?.width) : undefined,
    height: Number(raw?.height) > 0 ? Number(raw?.height) : undefined,
    created_by: String(raw?.created_by ?? ''),
    error_message: String(raw?.error_message ?? ''),
    ext,
    created_at: String(raw?.created_at ?? ''),
    started_at: String(raw?.started_at ?? ''),
    completed_at: String(raw?.completed_at ?? ''),
    updated_at: String(raw?.updated_at ?? ''),
  };
}

function pickDefinedParams<T extends object>(params: T): Partial<T> {
  return Object.fromEntries(
    Object.entries(params).filter(([, value]) => value !== undefined && value !== '')
  ) as Partial<T>;
}

export async function fetchClipTaskList(
  params?: ClipTaskListParams
): Promise<BaseResponse<ClipTaskListResult>> {
  const response = await request<BaseResponse<ClipTaskListResult>>('/v1/tasks', {
    method: 'get',
    params: params ? pickDefinedParams(params) : undefined,
  });

  return {
    ...response,
    data: {
      list: (response.data?.list ?? []).map(normalizeClipTaskItem),
      total: Number(response.data?.total ?? 0),
      page: response.data?.page,
      page_size: response.data?.page_size,
    },
  };
}

export async function fetchClipTaskDetail(taskId: string): Promise<BaseResponse<ClipTaskItem>> {
  const response = await request<BaseResponse<ClipTaskItem>>(`/v1/tasks/${taskId}`, {
    method: 'get',
  });

  return {
    ...response,
    data: normalizeClipTaskItem(response.data),
  };
}

export async function deleteClipTask(taskId: string): Promise<BaseResponse<null>> {
  return await request(`/v1/tasks/${taskId}`, {
    method: 'delete',
  });
}

/** 下载任务合成视频（优先接口文件流，其次任务上的 video_url） */
export async function downloadTaskVideo(
  task: Pick<ClipTaskItem, 'id' | 'video_url' | 'video_project_name'>
): Promise<void> {
  const filename = `${sanitizeDownloadFilename(task.video_project_name, task.id)}-合成视频.mp4`;
  const directUrl = task.video_url?.trim();

  try {
    await downloadTaskBlob(`/v1/tasks/${task.id}/video`, filename, '暂无合成视频');
    return;
  } catch (error) {
    if (directUrl) {
      await downloadRemoteFile(directUrl, filename);
      return;
    }
    throw error instanceof Error ? error : new Error('视频下载失败');
  }
}

/** 下载任务全部视频片段压缩包 */
export async function downloadTaskClipsTar(
  task: Pick<ClipTaskItem, 'id' | 'clips_tar_url' | 'video_project_name'>
): Promise<void> {
  const filename = `${sanitizeDownloadFilename(task.video_project_name, task.id)}-视频片段.tar`;
  const directUrl = task.clips_tar_url?.trim();

  try {
    await downloadTaskBlob(`/v1/tasks/${task.id}/clips-tar`, filename, '暂无视频片段压缩包');
    return;
  } catch (error) {
    if (directUrl) {
      await downloadRemoteFile(directUrl, filename);
      return;
    }
    throw error instanceof Error ? error : new Error('视频片段下载失败');
  }
}

export function canDownloadTaskOutputs(task: Pick<ClipTaskItem, 'status' | 'type'>) {
  if (task.status !== 'completed') return false;
  return task.type === 'draft' || task.type === 'ai_slice_draft';
}
