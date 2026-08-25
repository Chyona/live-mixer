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
import { getSliceProject, insertSliceProjectRecord } from './sliceProjectStore';
import type { SliceProjectSource } from '../src/services/sliceProject';

type MockClip0 = { start_time: number; end_time: number };

const AI_SLICE_PROJECT_MAX_MS = 30 * 60 * 1000;
const AI_SLICE_PROJECT_MIN_MS = 5 * 60 * 1000;

function mergeMockClips0(clips: MockClip0[]): MockClip0[] {
  const sorted = clips
    .filter((item) => item.end_time > item.start_time)
    .map((item) => ({ start_time: item.start_time, end_time: item.end_time }))
    .sort((a, b) => a.start_time - b.start_time || a.end_time - b.end_time);
  if (!sorted.length) return [];
  const out: MockClip0[] = [];
  let cur = { ...sorted[0] };
  for (let i = 1; i < sorted.length; i += 1) {
    const next = sorted[i];
    if (next.start_time <= cur.end_time) {
      cur.end_time = Math.max(cur.end_time, next.end_time);
    } else {
      out.push(cur);
      cur = { ...next };
    }
  }
  out.push(cur);
  return out;
}

function mockClipsDuration(clips: MockClip0[]): number {
  return clips.reduce((sum, item) => sum + Math.max(0, item.end_time - item.start_time), 0);
}

/** mock 无 ASR：按 30 分钟时长打包，尾组不足 5 分钟并入上一组。 */
function splitMockClips0(merged: MockClip0[]): MockClip0[][] {
  const total = mockClipsDuration(merged);
  if (total <= AI_SLICE_PROJECT_MAX_MS) return [merged];

  const groups: MockClip0[][] = [];
  let current: MockClip0[] = [];
  let currentDur = 0;
  for (const clip of merged) {
    let start = clip.start_time;
    while (start < clip.end_time) {
      if (currentDur >= AI_SLICE_PROJECT_MAX_MS) {
        groups.push(current);
        current = [];
        currentDur = 0;
      }
      const take = Math.min(clip.end_time - start, AI_SLICE_PROJECT_MAX_MS - currentDur);
      if (take <= 0) {
        groups.push(current);
        current = [];
        currentDur = 0;
        continue;
      }
      current.push({ start_time: start, end_time: start + take });
      currentDur += take;
      start += take;
    }
  }
  if (current.length) groups.push(current);

  while (groups.length >= 2 && mockClipsDuration(groups[groups.length - 1]) < AI_SLICE_PROJECT_MIN_MS) {
    const last = groups.pop()!;
    groups[groups.length - 1] = [...groups[groups.length - 1], ...last];
  }
  return groups;
}

function formatMockProjectName(prefix: string, index: number, total: number) {
  const d = new Date();
  const pad = (n: number) => String(n).padStart(2, '0');
  const stamp = `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())}_${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`;
  const base = `${prefix}_${stamp}`;
  return total > 1 ? `${base}_${index}` : base;
}

function clipsToSegments(clips: MockClip0[]) {
  return clips.map((clip, index) => ({
    id: `timeline-${index}-${clip.start_time}-${clip.end_time}`,
    speakerId: 'speaker-1',
    speakerName: '主播',
    text: '',
    start: clip.start_time / 1000,
    end: clip.end_time / 1000,
  }));
}

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
    response: ({
      body,
    }: {
      body: {
        video_project_id?: string | number;
        live_id?: number;
        prompt_id?: number;
        clips0?: MockClip0[];
        project_source?: SliceProjectSource;
      };
    }) => {
      const liveId = body?.live_id;
      const rawClips = Array.isArray(body?.clips0) ? body.clips0 : [];
      if (liveId && rawClips.length) {
        const merged = mergeMockClips0(rawClips);
        if (!merged.length) {
          return { code: 400, message: 'clips0 不能为空，请先设置待分析时间段', data: null };
        }
        const groups = splitMockClips0(merged);
        const existing = getSliceProject(String(liveId));
        const sourceVideoName = existing?.sourceVideoName ?? `素材${liveId}`;
        const list = groups.map((clips, index) => {
          const projectName = formatMockProjectName('AI选片', index + 1, groups.length);
          const projectId = `vp-ai-${Date.now()}-${index + 1}`;
          insertSliceProjectRecord({
            id: projectId,
            sourceVideoId: String(liveId),
            sourceVideoName,
            remarkName: '',
            projectName,
            projectSource: body.project_source ?? 'manual',
            segmentCount: clips.length,
            createdBy: 'admin',
            updatedAt: new Date().toISOString(),
            segments: clipsToSegments(clips),
          });
          const taskId = `ai-slice-task-${Date.now()}-${index + 1}`;
          createAiSliceTask({
            taskId,
            sourceVideoId: String(liveId),
            sourceVideoName,
            promptName: projectName,
            clips: clips.map((clip) => ({ start: clip.start_time / 1000, end: clip.end_time / 1000 })),
            segments: clipsToSegments(clips),
            videoProjectId: projectId,
          });
          return { id: taskId, type: 'ai_slice', status: 'pending' };
        });
        return { code: 0, message: '', data: { list, total: list.length } };
      }

      const videoProjectId = body?.video_project_id;
      if (videoProjectId == null || videoProjectId === '') {
        return { code: 400, message: 'live_id 或 video_project_id 不能为空', data: null };
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
        data: { list: [{ id: taskId, type: 'ai_slice', status: 'pending' }], total: 1, task_id: taskId },
      };
    },
  },
  {
    url: `${API_PREFIX}/v1/tasks/ai-slice-draft`,
    method: 'post',
    response: ({
      body,
    }: {
      body: {
        video_project_id?: string | number;
        live_id?: number;
        prompt_id?: number;
        clips0?: MockClip0[];
        project_source?: SliceProjectSource;
      };
    }) => {
      const liveId = body?.live_id;
      const rawClips = Array.isArray(body?.clips0) ? body.clips0 : [];
      if (liveId && rawClips.length) {
        const merged = mergeMockClips0(rawClips);
        if (!merged.length) {
          return { code: 400, message: 'clips0 不能为空，请先设置待分析时间段', data: null };
        }
        const groups = splitMockClips0(merged);
        const existing = getSliceProject(String(liveId));
        const sourceVideoName = existing?.sourceVideoName ?? `素材${liveId}`;
        const list = groups.map((clips, index) => {
          const projectName = formatMockProjectName('一键成片', index + 1, groups.length);
          const projectId = `vp-draft-${Date.now()}-${index + 1}`;
          insertSliceProjectRecord({
            id: projectId,
            sourceVideoId: String(liveId),
            sourceVideoName,
            remarkName: '',
            projectName,
            projectSource: body.project_source ?? 'timeline',
            segmentCount: clips.length,
            createdBy: 'admin',
            updatedAt: new Date().toISOString(),
            segments: clipsToSegments(clips),
          });
          const taskId = `clip-task-${Date.now()}-${index + 1}`;
          createClipTask({
            taskId,
            sourceVideoId: String(liveId),
            sourceVideoName,
            m3u8Url: '',
            clipName: projectName,
            videoProjectId: projectId,
          });
          return { id: taskId, type: 'ai_slice_draft', status: 'pending' };
        });
        return { code: 0, message: '', data: { list, total: list.length } };
      }

      const videoProjectId = body?.video_project_id;
      if (videoProjectId == null || videoProjectId === '') {
        return { code: 400, message: 'live_id 或 video_project_id 不能为空', data: null };
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
        data: { list: [{ id: taskId, type: 'ai_slice_draft', status: 'pending' }], total: 1, task_id: taskId },
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
