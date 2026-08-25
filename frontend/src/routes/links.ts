import type { SliceProjectSource } from '~/services/sliceProject';

export type SliceEditorEntryFrom = 'source-videos' | 'slices' | 'tasks';

export const LIVE_SLICE_PATH = '/videos-slice';
export const VIDEOS_MANUAL_SLICE_PATH = '/videos-manual-slice';

export type SliceEditorLinkOptions = {
  /** 切片项目 id；从项目管理进入编辑时必传 */
  projectId?: string | number | null;
};

function appendProjectId(path: string, options?: SliceEditorLinkOptions) {
  const projectId = options?.projectId;
  if (projectId == null || projectId === '') return path;
  const search = new URLSearchParams({ projectId: String(projectId) });
  return `${path}?${search.toString()}`;
}

export function buildSourceVideoSliceLink(
  sourceVideoId: string,
  options?: SliceEditorLinkOptions
) {
  return appendProjectId(`${LIVE_SLICE_PATH}/${sourceVideoId}`, options);
}

export function buildManualVideoSliceLink(
  sourceVideoId: string,
  options?: SliceEditorLinkOptions
) {
  return appendProjectId(`${VIDEOS_MANUAL_SLICE_PATH}/${sourceVideoId}`, options);
}

export function buildSliceProjectEditLink(params: {
  /** 源视频 id（路径参数） */
  liveId: string | number;
  /** 剪辑项目 id（查询参数 projectId） */
  id: string | number;
  projectSource?: SliceProjectSource;
}) {
  const { liveId, id, projectSource = 'manual' } = params;
  const options = { projectId: id };
  return projectSource === 'timeline'
    ? buildSourceVideoSliceLink(String(liveId), options)
    : buildManualVideoSliceLink(String(liveId), options);
}

export function getSliceEditorEntryFrom(state: unknown): SliceEditorEntryFrom | undefined {
  return (state as { from?: SliceEditorEntryFrom } | null)?.from;
}

/** 解析 URL query 中的 projectId（正整数）；非法则返回 null */
export function parseProjectId(value: string | null | undefined): number | null {
  if (value == null || value.trim() === '') return null;
  const id = Number(value.trim());
  return Number.isInteger(id) && id > 0 ? id : null;
}

/** 项目管理列表链接；可带 keyword 作为搜索条件（如源视频名称） */
export function buildSlicesListLink(options?: { keyword?: string | null }) {
  const keyword = options?.keyword?.trim();
  if (!keyword) return '/slices';
  return `/slices?${new URLSearchParams({ keyword }).toString()}`;
}

/** 任务管理列表链接；可带 keyword 作为搜索条件（如源视频名称 / 项目名称） */
export function buildTasksListLink(options?: { keyword?: string | null }) {
  const keyword = options?.keyword?.trim();
  if (!keyword) return '/tasks';
  return `/tasks?${new URLSearchParams({ keyword }).toString()}`;
}
