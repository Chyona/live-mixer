import type { MockMethod } from 'vite-plugin-mock';
import { matchListKeywords, parseListKeywords } from '../src/utils/listKeywords';
import { API_PREFIX } from './_config';
import {
  clipTaskStore,
  createAiSliceTask,
  createClipTask,
  deleteClipTask,
  toPublicClipTask,
} from './clipTaskStore';
import { getSliceProject } from './sliceProjectStore';

function findTaskById(id: string) {
  return (
    clipTaskStore.find((item) => item.taskId === id) ??
    clipTaskStore.find((item) => String(Number(item.taskId.replace(/\D/g, '')) || '') === id) ??
    null
  );
}

function filterClipTasks(query: Record<string, string | string[] | undefined>) {
  const startDate =
    typeof query.start_date === 'string'
      ? query.start_date
      : typeof query.date === 'string'
        ? query.date
        : undefined;
  const endDate =
    typeof query.end_date === 'string'
      ? query.end_date
      : typeof query.dateEnd === 'string'
        ? query.dateEnd
        : undefined;
  const keyword =
    typeof query.keywords === 'string'
      ? query.keywords
      : typeof query.keyword === 'string'
        ? query.keyword
        : undefined;
  const status = typeof query.status === 'string' ? query.status : undefined;
  const type = typeof query.type === 'string' ? query.type : undefined;
  const titleKeywords = parseListKeywords(keyword);

  return clipTaskStore.filter((task) => {
    const createdDate = task.createdAt.slice(0, 10);
    if (startDate && createdDate < startDate) return false;
    if (endDate && createdDate > endDate) return false;

    const publicStatus =
      task.status === 'success' ? 'completed' : task.status === 'running' ? 'processing' : task.status;
    if (status && publicStatus !== status) return false;

    const publicType =
      task.taskType === 'ai_slice_select'
        ? 'ai_slice'
        : task.taskType === 'draft'
          ? 'draft'
          : task.taskType === 'clip_generate'
            ? 'ai_slice_draft'
            : task.taskType;
    if (type && publicType !== type) return false;

    const titleText = `${task.sourceVideoName} ${task.clipName} ${task.promptName ?? ''}`;
    if (!matchListKeywords(titleText, titleKeywords)) return false;

    return true;
  });
}

export default [
  {
    url: `${API_PREFIX}/v1/tasks`,
    method: 'get',
    response: ({ query }: { query: Record<string, string | string[] | undefined> }) => {
      const filtered = filterClipTasks(query);
      const page = Number(query.page || 1);
      const pageSize = Number(query.page_size || query.pageSize || 10);
      const start = (page - 1) * pageSize;

      return {
        code: 0,
        message: 'success',
        data: {
          list: filtered.slice(start, start + pageSize).map(toPublicClipTask),
          total: filtered.length,
          page,
          page_size: pageSize,
        },
      };
    },
  },
  {
    url: `${API_PREFIX}/v1/tasks/:id`,
    method: 'get',
    response: ({ query }: { query: { id: string } }) => {
      const task = findTaskById(query.id);
      if (!task) {
        return { code: 404, message: '任务不存在', data: null };
      }
      return { code: 0, message: '', data: toPublicClipTask(task) };
    },
  },
  {
    url: `${API_PREFIX}/v1/tasks/:id/video`,
    method: 'get',
    response: ({ query }: { query: { id: string } }) => {
      const task = findTaskById(query.id);
      if (!task) {
        return { code: 404, message: '任务不存在', data: null };
      }
      const url = task.videoUrls[0]?.trim() || '';
      if (!url) {
        return { code: 404, message: '暂无合成视频', data: null };
      }
      return { code: 0, message: '', data: { url } };
    },
  },
  {
    url: `${API_PREFIX}/v1/tasks/:id/clips-tar`,
    method: 'get',
    response: ({ query }: { query: { id: string } }) => {
      const task = findTaskById(query.id);
      if (!task) {
        return { code: 404, message: '任务不存在', data: null };
      }
      if (task.status !== 'success' || !task.videoUrls[0]) {
        return { code: 404, message: '暂无视频片段压缩包', data: null };
      }
      return {
        code: 0,
        message: '',
        data: { url: `https://mock.example.com/clips/${task.taskId}.tar` },
      };
    },
  },
  {
    url: `${API_PREFIX}/v1/tasks/:id`,
    method: 'delete',
    response: ({ query }: { query: { id: string } }) => {
      const deleted = deleteClipTask(query.id);
      if (!deleted) {
        return { code: 404, message: '任务不存在', data: null };
      }
      return { code: 0, message: '', data: null };
    },
  },
  {
    url: `${API_PREFIX}/v1/tasks/ai-slice`,
    method: 'post',
    response: ({ body }: { body: { video_project_id?: string | number } }) => {
      const videoProjectId = body?.video_project_id;
      if (videoProjectId == null || videoProjectId === '') {
        return { code: 400, message: '缺少 video_project_id', data: null };
      }

      const project = getSliceProject(String(videoProjectId));
      if (!project) {
        return { code: 404, message: '剪辑项目不存在', data: null };
      }

      const clips = project.segments.map((segment) => ({
        start: Math.round(segment.start),
        end: Math.round(segment.end),
      }));
      if (!clips.length) {
        return { code: 400, message: '项目中没有可选片段', data: null };
      }

      const taskId = `ai-slice-task-${Date.now()}`;
      createAiSliceTask({
        taskId,
        sourceVideoId: project.sourceVideoId,
        sourceVideoName: project.sourceVideoName,
        promptName: project.projectName,
        clips,
        segments: project.segments,
        videoProjectId: String(videoProjectId),
      });

      return {
        code: 0,
        message: '',
        data: { task_id: taskId },
      };
    },
  },
  {
    url: `${API_PREFIX}/v1/tasks/ai-slice-draft`,
    method: 'post',
    response: ({ body }: { body: { video_project_id?: string | number } }) => {
      const videoProjectId = body?.video_project_id;
      if (videoProjectId == null || videoProjectId === '') {
        return { code: 400, message: '缺少 video_project_id', data: null };
      }

      const project = getSliceProject(String(videoProjectId));
      if (!project) {
        return { code: 404, message: '剪辑项目不存在', data: null };
      }

      const taskId = `clip-task-${Date.now()}`;
      createClipTask({
        taskId,
        sourceVideoId: project.sourceVideoId,
        sourceVideoName: project.sourceVideoName,
        m3u8Url: '',
        clipName: project.projectName,
        videoProjectId: String(videoProjectId),
      });

      return {
        code: 0,
        message: '',
        data: { task_id: taskId },
      };
    },
  },
  {
    url: `${API_PREFIX}/v1/tasks/draft`,
    method: 'post',
    response: ({
      body,
    }: {
      body: { video_project_id?: string | number; enable_captions?: boolean };
    }) => {
      const videoProjectId = body?.video_project_id;
      if (videoProjectId == null || videoProjectId === '') {
        return { code: 400, message: '缺少 video_project_id', data: null };
      }

      const project = getSliceProject(String(videoProjectId));
      if (!project) {
        return { code: 404, message: '剪辑项目不存在', data: null };
      }

      const taskId = `draft-task-${Date.now()}`;
      createClipTask({
        taskId,
        sourceVideoId: project.sourceVideoId,
        sourceVideoName: project.sourceVideoName,
        m3u8Url: '',
        clipName: project.projectName,
        taskType: 'draft',
        videoProjectId: String(videoProjectId),
      });

      return {
        code: 0,
        message: '',
        data: { task_id: taskId, enable_captions: Boolean(body?.enable_captions) },
      };
    },
  },
] as MockMethod[];
