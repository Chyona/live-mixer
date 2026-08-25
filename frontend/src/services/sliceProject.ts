import type { SelectedCopySegment } from '~/pages/ManualVideoSlice/types';
import type { BaseResponse } from './types';
import { request } from './http';

export type SliceProjectSource = 'timeline' | 'manual';

/** 接口 clips 时间单位：毫秒 */
export interface SliceProjectClip {
  start_time: number;
  end_time: number;
  /** 人工切片文案（clips1） */
  text?: string;
  /** 时间轴片段标题（clips0，来自 asr_summaries.title） */
  title?: string;
}


/** 新建剪辑项目请求体 */
export interface CreateSliceProjectParams {
  live_id: number;
  name: string;
  remark?: string;
  prompt_id?: number;
  /**
   * 项目来源：
   * - timeline：时间轴一键成片保存
   * - manual：人工切片保存
   */
  project_source: SliceProjectSource;
  /** 是否生成字幕（成片选项） */
  enable_captions?: boolean;
  clips0?: SliceProjectClip[];
  clips1?: SliceProjectClip[];
}

/** 更新剪辑项目请求体，均为可选 */
export type UpdateSliceProjectParams = Partial<CreateSliceProjectParams>;



export interface SliceProjectListParams {
  /** 关键词，英文逗号分隔，匹配 name/remark */
  keywords?: string;
  /** 开始日期 YYYY-MM-DD */
  start_date?: string;
  /** 结束日期 YYYY-MM-DD */
  end_date?: string;
  page?: number;
  page_size?: number;
}

/**
 * 剪辑项目（与接口返回结构一致）
 */
export type SliceProject = CreateSliceProjectParams & {
  id: number;
  live_name: string;
  created_by: string;
  draft_url: string;
  video_url: string;
  ext: string;
  /** 当前源视频关联的任务数量 */
  task_count: number;
  /** 视频宽度（像素） */
  width?: number;
  /** 视频高度（像素） */
  height?: number;
  created_at: string;
  updated_at: string;
};

/** 根据宽高推导列表展示比例：竖屏 9:16，横屏 16:9 */
export function getSliceProjectAspectRatio(
  project: Pick<SliceProject, 'width' | 'height'>
): '9:16' | '16:9' | '-' {
  const width = Number(project.width);
  const height = Number(project.height);
  if (!Number.isFinite(width) || !Number.isFinite(height) || width <= 0 || height <= 0) {
    return '-';
  }
  if (height > width) return '9:16';
  if (width > height) return '16:9';
  return '-';
}

/** 详情：在接口字段基础上，补充 UI 用 segments */
export interface SliceProjectDetail extends SliceProject {
  segments: SelectedCopySegment[];
}

export interface SliceProjectListResult {
  list: SliceProject[];
  total: number;
  page?: number;
  page_size?: number;
}

function pickDefinedParams<T extends object>(params: T): Partial<T> {
  return Object.fromEntries(
    Object.entries(params).filter(([, value]) => value !== undefined)
  ) as Partial<T>;
}

function clipsToUiSegments(
  clips: SliceProjectClip[] | undefined,
  prefix: string
): SelectedCopySegment[] {
  if (!clips?.length) return [];
  return clips.map((clip, index) => {
    const start = (clip.start_time ?? 0) / 1000;
    const end = (clip.end_time ?? 0) / 1000;
    return {
      id: `${prefix}-${index}-${Math.round(start * 1000)}-${Math.round(end * 1000)}`,
      speakerId: '1',
      speakerName: '',
      text: clip.text?.trim() ?? '',
      start,
      end,
      originStart: start,
      originEnd: end,
    };
  });
}

export function getSliceProjectSource(project: {
  project_source?: SliceProjectSource;
  clips0?: SliceProjectClip[];
  clips1?: SliceProjectClip[];
}): SliceProjectSource {
  if (project.project_source === 'timeline' || project.project_source === 'manual') {
    return project.project_source;
  }
  const hasManual = (project.clips1?.length ?? 0) > 0;
  const hasTimeline = (project.clips0?.length ?? 0) > 0;
  if (hasManual && !hasTimeline) return 'manual';
  if (hasTimeline) return 'timeline';
  return 'manual';
}

/** 项目列表「片段数」统一取 clips1 */
export function getSliceProjectSegmentCount(
  project: Pick<SliceProject, 'clips1'>
): number {
  return project.clips1?.length ?? 0;
}

export function clipsToSliceSegments(
  project: Pick<SliceProject, 'clips0' | 'clips1'>
): SelectedCopySegment[] {
  // 文案预览 / 人工切片只使用 clips1；clips0 为时间轴选段，标题用 title
  return clipsToUiSegments(project.clips1, 'manual');
}

function normalizeTaskCount(raw: Partial<SliceProject> & Record<string, unknown>): number {
  const value = Number(raw.task_count ?? raw.taskCount ?? 0);
  return Number.isFinite(value) && value > 0 ? Math.floor(value) : 0;
}

/** 规范化接口返回，补齐默认值 */
export function normalizeSliceProject(raw: Partial<SliceProject> | null | undefined): SliceProject {
  const record = (raw ?? {}) as Partial<SliceProject> & Record<string, unknown>;
  return {
    id: Number(raw?.id ?? 0),
    name: String(raw?.name ?? ''),
    live_id: Number(raw?.live_id ?? 0),
    live_name: String(raw?.live_name ?? ''),
    prompt_id: Number(raw?.prompt_id ?? 0),
    created_by: String(raw?.created_by ?? ''),
    created_at: String(raw?.created_at ?? ''),
    updated_at: String(raw?.updated_at ?? ''),
    remark: String(raw?.remark ?? ''),
    draft_url: String(raw?.draft_url ?? ''),
    video_url: String(raw?.video_url ?? ''),
    ext: String(raw?.ext ?? ''),
    task_count: normalizeTaskCount(record),
    width: Number(raw?.width) > 0 ? Number(raw?.width) : undefined,
    height: Number(raw?.height) > 0 ? Number(raw?.height) : undefined,
    clips0: Array.isArray(raw?.clips0) ? raw.clips0 : [],
    clips1: Array.isArray(raw?.clips1) ? raw.clips1 : [],
    enable_captions: Boolean(raw?.enable_captions),
    project_source: getSliceProjectSource({
      project_source: raw?.project_source,
      clips0: Array.isArray(raw?.clips0) ? raw.clips0 : [],
      clips1: Array.isArray(raw?.clips1) ? raw.clips1 : [],
    }),
  };
}

function normalizeSliceProjectDetail(
  raw: Partial<SliceProject> | null | undefined
): SliceProjectDetail {
  const project = normalizeSliceProject(raw);
  return {
    ...project,
    segments: clipsToSliceSegments(project),
  };
}

/** 内部秒级片段 → 接口毫秒级 clips */
export function toSliceProjectClips(
  segments: Array<{ start: number; end: number; text?: string }>
): SliceProjectClip[] {
  return segments.map((segment) => ({
    start_time: Math.round(segment.start * 1000),
    end_time: Math.round(segment.end * 1000),
    text: segment.text?.trim() || undefined,
  }));
}

export async function fetchSliceProjectList(
  params?: SliceProjectListParams
): Promise<BaseResponse<SliceProjectListResult>> {
  const response = await request<BaseResponse<SliceProjectListResult>>('/v1/video-projects', {
    method: 'get',
    params,
  });

  return {
    ...response,
    data: {
      list: (response.data?.list ?? []).map(normalizeSliceProject),
      total: Number(response.data?.total ?? 0),
      page: response.data?.page,
      page_size: response.data?.page_size,
    },
  };
}

/** 按项目 id 拉详情 */
export async function fetchSliceProjectDetail(
  projectId: string | number
): Promise<BaseResponse<SliceProjectDetail>> {
  const response = await request<BaseResponse<SliceProject>>(`/v1/video-projects/${projectId}`, {
    method: 'get',
  });

  return {
    ...response,
    data: normalizeSliceProjectDetail(response.data),
  };
}

/**
 * 新建剪辑项目（无项目 id）。
 * POST /v1/video-projects
 */
export async function saveSliceProject(
  params: CreateSliceProjectParams
): Promise<BaseResponse<SliceProjectDetail>> {
  const response = await request<BaseResponse<SliceProject>>('/v1/video-projects', {
    method: 'post',
    data: params,
  });

  return {
    ...response,
    data: normalizeSliceProjectDetail(response.data),
  };
}

/**
 * 更新已有剪辑项目（有项目 id）。
 * PUT /v1/video-projects/:id — 仅提交传入的字段。
 */
export async function updateSliceProject(
  projectId: string | number,
  params: UpdateSliceProjectParams
): Promise<BaseResponse<SliceProjectDetail>> {
  const response = await request<BaseResponse<SliceProject>>(`/v1/video-projects/${projectId}`, {
    method: 'put',
    data: pickDefinedParams(params),
  });

  return {
    ...response,
    data: normalizeSliceProjectDetail(response.data),
  };
}

/** 按项目 id 删除 */
export async function deleteSliceProject(
  projectId: string | number
): Promise<BaseResponse<null>> {
  return await request(`/v1/video-projects/${projectId}`, {
    method: 'delete',
  });
}

/** 项目进行中/待进行任务（切片页只读锁） */
export interface SliceProjectRunningTaskItem {
  id?: string | number;
  status?: string;
  [key: string]: unknown;
}

export interface SliceProjectRunningTasks {
  list: SliceProjectRunningTaskItem[];
  /** 进行中 + 待进行任务总数 */
  total: number;
}

function normalizeRunningTasks(raw: unknown): SliceProjectRunningTasks {
  if (!raw || typeof raw !== 'object' || Array.isArray(raw)) {
    return { list: [], total: 0 };
  }

  const record = raw as Record<string, unknown>;
  const list = Array.isArray(record.list)
    ? (record.list as SliceProjectRunningTaskItem[])
    : [];
  const totalRaw = Number(record.total);
  const total = Number.isFinite(totalRaw) ? Math.max(0, Math.floor(totalRaw)) : list.length;

  return { list, total };
}

/**
 * 查询剪辑项目进行中/待进行任务。
 * GET /v1/video-projects/:id/running-tasks
 * 返回：{ list, total }
 */
export async function fetchSliceProjectRunningTasks(
  projectId: string | number
): Promise<BaseResponse<SliceProjectRunningTasks>> {
  const response = await request<BaseResponse<SliceProjectRunningTasks>>(
    `/v1/video-projects/${projectId}/running-tasks`,
    { method: 'get' }
  );

  return {
    ...response,
    data: normalizeRunningTasks(response.data),
  };
}
