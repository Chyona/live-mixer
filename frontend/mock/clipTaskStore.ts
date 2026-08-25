import type { SelectedCopySegment } from '../src/pages/ManualVideoSlice/types';
import { upsertSliceProject } from './sliceProjectStore';

export type GenerationTaskType = 'clip_generate' | 'ai_slice_select' | 'draft';

export type StoredClipTask = {
  taskId: string;
  taskType: GenerationTaskType;
  clipName: string;
  sourceVideoId: string;
  sourceVideoName: string;
  /** 关联剪辑项目 id（可选） */
  videoProjectId?: string;
  m3u8Url: string;
  status: 'pending' | 'processing' | 'running' | 'success' | 'failed';
  progress: number;
  videoUrls: string[];
  draftUrls: string[];
  message: string | null;
  createdAt: string;
  pollCount: number;
  promptName?: string | null;
  aiClips?: Array<{ start: number; end: number }>;
  aiSegments?: SelectedCopySegment[];
  segmentCount?: number;
};

const MOCK_VIDEO_URL =
  'https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/BigBuckBunny.mp4';

export const clipTaskStore: StoredClipTask[] = [];

function seedMockClipTasks() {
  if (clipTaskStore.length > 0) return;

  const now = Date.now();

  clipTaskStore.push(
    {
      taskId: 'clip-task-seed-001',
      taskType: 'clip_generate',
      clipName: '周末游戏直播回放 精彩片段',
      sourceVideoId: 'sv-001',
      sourceVideoName: '周末游戏直播回放',
      m3u8Url: MOCK_VIDEO_URL,
      status: 'success',
      progress: 100,
      videoUrls: [MOCK_VIDEO_URL],
      draftUrls: ['https://mock.example.com/drafts/clip-task-seed-001.json'],
      message: null,
      createdAt: new Date(now - 1000 * 60 * 60 * 26).toISOString(),
      pollCount: 5,
    },
    {
      taskId: 'clip-task-seed-002',
      taskType: 'clip_generate',
      clipName: '新品发布会直播 剪辑项目',
      sourceVideoId: '2',
      sourceVideoName: '新品发布会直播',
      videoProjectId: 'vp-2',
      m3u8Url: 'https://example.com/live/product_launch.m3u8',
      status: 'running',
      progress: 45,
      videoUrls: [],
      draftUrls: [],
      message: null,
      createdAt: new Date(now - 1000 * 60 * 12).toISOString(),
      pollCount: 1,
    },
    {
      taskId: 'clip-task-seed-003',
      taskType: 'clip_generate',
      clipName: '户外探店直播 成片',
      sourceVideoId: 'sv-003',
      sourceVideoName: '户外探店直播',
      m3u8Url: 'https://example.com/videos/explore_shop.mp4',
      status: 'failed',
      progress: 42,
      videoUrls: [],
      draftUrls: [],
      message: '视频流解析失败：源地址无法访问或已过期',
      createdAt: new Date(now - 1000 * 60 * 60 * 5).toISOString(),
      pollCount: 2,
    },
    {
      taskId: 'ai-slice-task-seed-001',
      taskType: 'ai_slice_select',
      clipName: '周末游戏直播回放 AI选片',
      sourceVideoId: 'sv-001',
      sourceVideoName: '周末游戏直播回放',
      m3u8Url: '',
      status: 'success',
      progress: 100,
      videoUrls: [],
      draftUrls: [],
      message: null,
      createdAt: new Date(now - 1000 * 60 * 60 * 8).toISOString(),
      pollCount: 4,
      promptName: '高光片段筛选',
      segmentCount: 6,
      aiSegments: [
        {
          id: 'ai-seg-1',
          speakerId: 'host',
          speakerName: '主播',
          text: '今天给大家带来的是春季上新系列，性价比非常高。',
          start: 4,
          end: 9,
        },
        {
          id: 'ai-seg-2',
          speakerId: 'guest',
          speakerName: '嘉宾',
          text: '很多老用户反馈上身效果比上一代更舒适。',
          start: 13.5,
          end: 18,
        },
      ],
    },
    {
      taskId: 'ai-slice-task-seed-002',
      taskType: 'ai_slice_select',
      clipName: '新品发布会直播 AI选片',
      sourceVideoId: 'sv-002',
      sourceVideoName: '新品发布会直播',
      m3u8Url: '',
      status: 'running',
      progress: 55,
      videoUrls: [],
      draftUrls: [],
      message: null,
      createdAt: new Date(now - 1000 * 60 * 20).toISOString(),
      pollCount: 2,
      promptName: '产品卖点提取',
      aiClips: [{ start: 60, end: 180 }],
    },
    {
      taskId: 'draft-task-seed-001',
      taskType: 'draft',
      clipName: '人工切片_2026-07-16_10:20:00',
      sourceVideoId: 'sv-004',
      sourceVideoName: '视频4',
      m3u8Url: MOCK_VIDEO_URL,
      status: 'pending',
      progress: 0,
      videoUrls: [],
      draftUrls: [],
      message: null,
      createdAt: new Date(now - 1000 * 60 * 3).toISOString(),
      pollCount: 0,
    },
    {
      taskId: 'draft-task-seed-002',
      taskType: 'draft',
      clipName: '人工切片_2026-07-16_09:45:12',
      sourceVideoId: 'sv-001',
      sourceVideoName: '周末游戏直播回放',
      m3u8Url: MOCK_VIDEO_URL,
      status: 'pending',
      progress: 0,
      videoUrls: [],
      draftUrls: [],
      message: null,
      createdAt: new Date(now - 1000 * 60 * 8).toISOString(),
      pollCount: 0,
    },
    {
      taskId: 'draft-task-seed-003',
      taskType: 'draft',
      clipName: '人工切片_2026-07-15_18:30:00',
      sourceVideoId: 'sv-002',
      sourceVideoName: '新品发布会直播',
      m3u8Url: MOCK_VIDEO_URL,
      status: 'running',
      progress: 28,
      videoUrls: [],
      draftUrls: [],
      message: null,
      createdAt: new Date(now - 1000 * 60 * 35).toISOString(),
      pollCount: 1,
    },
    {
      taskId: 'draft-task-seed-004',
      taskType: 'draft',
      clipName: '人工切片_2026-07-15_16:05:40',
      sourceVideoId: 'sv-003',
      sourceVideoName: '户外探店直播',
      m3u8Url: MOCK_VIDEO_URL,
      status: 'success',
      progress: 100,
      videoUrls: [],
      draftUrls: ['https://mock.example.com/drafts/draft-task-seed-004.json'],
      message: null,
      createdAt: new Date(now - 1000 * 60 * 60 * 10).toISOString(),
      pollCount: 4,
    },
    {
      taskId: 'draft-task-seed-005',
      taskType: 'draft',
      clipName: '人工切片_2026-07-14_21:12:08',
      sourceVideoId: 'sv-004',
      sourceVideoName: '视频4',
      m3u8Url: MOCK_VIDEO_URL,
      status: 'failed',
      progress: 15,
      videoUrls: [],
      draftUrls: [],
      message: '草稿生成失败：片段总时长过短',
      createdAt: new Date(now - 1000 * 60 * 60 * 30).toISOString(),
      pollCount: 2,
    },
    {
      taskId: 'clip-task-seed-004',
      taskType: 'clip_generate',
      clipName: '一键成片_2026-07-16_11:00:00',
      sourceVideoId: 'sv-001',
      sourceVideoName: '周末游戏直播回放',
      m3u8Url: MOCK_VIDEO_URL,
      status: 'pending',
      progress: 0,
      videoUrls: [],
      draftUrls: [],
      message: null,
      createdAt: new Date(now - 1000 * 60 * 2).toISOString(),
      pollCount: 0,
    }
  );
}

seedMockClipTasks();

export function createClipTask(input: {
  taskId: string;
  sourceVideoId: string;
  sourceVideoName: string;
  m3u8Url: string;
  clipName?: string;
  taskType?: GenerationTaskType;
  videoProjectId?: string;
}) {
  clipTaskStore.unshift({
    taskId: input.taskId,
    taskType: input.taskType || 'clip_generate',
    clipName: input.clipName?.trim() || `${input.sourceVideoName} 成片`,
    sourceVideoId: input.sourceVideoId,
    sourceVideoName: input.sourceVideoName,
    videoProjectId: input.videoProjectId,
    m3u8Url: input.m3u8Url,
    status: 'pending',
    progress: 0,
    videoUrls: [],
    draftUrls: [],
    message: null,
    createdAt: new Date().toISOString(),
    pollCount: 0,
  });
}

export function createAiSliceTask(input: {
  taskId: string;
  sourceVideoId: string;
  sourceVideoName: string;
  promptName: string;
  clips: Array<{ start: number; end: number }>;
  segments?: SelectedCopySegment[];
  videoProjectId?: string;
}) {
  clipTaskStore.unshift({
    taskId: input.taskId,
    taskType: 'ai_slice_select',
    clipName: `${input.sourceVideoName} AI选片`,
    sourceVideoId: input.sourceVideoId,
    sourceVideoName: input.sourceVideoName,
    videoProjectId: input.videoProjectId,
    m3u8Url: '',
    status: 'pending',
    progress: 0,
    videoUrls: [],
    draftUrls: [],
    message: null,
    createdAt: new Date().toISOString(),
    pollCount: 0,
    promptName: input.promptName,
    aiClips: input.clips,
    aiSegments: input.segments,
    segmentCount: input.segments?.length ?? 0,
  });
}

function advanceAiSliceTask(task: StoredClipTask) {
  task.pollCount += 1;

  if (task.pollCount <= 1) {
    task.status = 'processing';
    task.progress = 20;
    return;
  }

  if (task.pollCount <= 2) {
    task.status = 'running';
    task.progress = 55;
    return;
  }

  if (task.pollCount <= 3) {
    task.status = 'running';
    task.progress = 85;
    return;
  }

  if (!task.aiSegments?.length) {
    task.status = 'failed';
    task.message = '所选时间范围内未匹配到文案片段';
    task.progress = task.progress || 85;
    return;
  }

  task.status = 'success';
  task.progress = 100;
  task.segmentCount = task.aiSegments.length;
  task.message = null;

  upsertSliceProject({
    sourceVideoId: task.sourceVideoId,
    sourceVideoName: task.sourceVideoName,
    projectName: `${task.sourceVideoName} 剪辑项目`,
    segmentCount: task.segmentCount,
  });
}

export function getClipTaskPollResult(taskId: string) {
  const task = clipTaskStore.find((item) => item.taskId === taskId);
  if (!task) return null;

  if (task.status === 'failed' || task.status === 'success') {
    return {
      task_id: task.taskId,
      status: task.status,
      progress: task.progress,
      video_urls: task.videoUrls,
      draft_urls: task.draftUrls,
      error: task.message,
    };
  }

  if (task.taskType === 'ai_slice_select') {
    advanceAiSliceTask(task);
    return {
      task_id: task.taskId,
      status: task.status,
      progress: task.progress,
      video_urls: task.videoUrls,
      draft_urls: task.draftUrls,
      error: task.message,
    };
  }

  task.pollCount += 1;

  if (task.pollCount <= 2) {
    task.status = 'processing';
    task.progress = task.pollCount * 25;
  } else if (task.pollCount <= 4) {
    task.status = 'running';
    task.progress = 50 + task.pollCount * 10;
  } else {
    task.status = 'success';
    task.progress = 100;
    task.videoUrls = [MOCK_VIDEO_URL];
    task.draftUrls = [`https://mock.example.com/drafts/${task.taskId}.json`];
    task.message = null;
  }

  return {
    task_id: task.taskId,
    status: task.status,
    progress: task.progress,
    video_urls: task.videoUrls,
    draft_urls: task.draftUrls,
    error: task.message,
  };
}

function findStoredClipTask(taskId: string) {
  return (
    clipTaskStore.find((item) => item.taskId === taskId) ??
    clipTaskStore.find((item) => String(Number(item.taskId.replace(/\D/g, '')) || '') === taskId) ??
    null
  );
}

export function markClipTaskFailed(taskId: string, message: string) {
  const task = findStoredClipTask(taskId);
  if (!task) return;
  task.status = 'failed';
  task.message = message;
}

export function deleteClipTask(taskId: string) {
  const task = findStoredClipTask(taskId);
  if (!task) return false;
  const index = clipTaskStore.indexOf(task);
  if (index < 0) return false;
  clipTaskStore.splice(index, 1);
  return true;
}

/** 统计某源视频关联的任务数（兼容数字 id 与 sv-001 形式） */
export function countClipTasksBySourceVideoId(sourceVideoId: string | number) {
  const id = String(sourceVideoId);
  const numericId = Number(id.replace(/\D/g, '')) || 0;
  return clipTaskStore.filter((task) => {
    if (task.sourceVideoId === id) return true;
    if (numericId <= 0) return false;
    return (Number(task.sourceVideoId.replace(/\D/g, '')) || 0) === numericId;
  }).length;
}

/** 按剪辑项目统计任务总数 / 进行中数量 */
export function countClipTasksByProject(project: {
  id: string;
  sourceVideoId: string;
  projectName: string;
}) {
  const projectKey = String(project.id);
  const projectNumericId = Number(projectKey.replace(/\D/g, '')) || 0;
  const tasks = clipTaskStore.filter((task) => {
    if (task.videoProjectId) {
      if (String(task.videoProjectId) === projectKey) return true;
      if (
        projectNumericId > 0 &&
        (Number(String(task.videoProjectId).replace(/\D/g, '')) || 0) === projectNumericId
      ) {
        return true;
      }
    }
    // 兼容：同源视频且名称对得上
    if (task.sourceVideoId !== project.sourceVideoId) return false;
    return task.clipName === project.projectName || task.promptName === project.projectName;
  });

  const active = tasks.filter(
    (task) => task.status === 'pending' || task.status === 'processing' || task.status === 'running'
  ).length;

  return { task_count: tasks.length, active_task_count: active };
}

export function toPublicClipTask(task: StoredClipTask) {
  const numericId = Number(String(task.taskId).replace(/\D/g, '')) || Date.now();
  const type =
    task.taskType === 'ai_slice_select'
      ? 'ai_slice'
      : task.taskType === 'draft'
        ? 'draft'
        : task.taskType === 'clip_generate'
          ? 'ai_slice_draft'
          : task.taskType;
  const status =
    task.status === 'success'
      ? 'completed'
      : task.status === 'running'
        ? 'processing'
        : task.status;
  const videoUrl = task.videoUrls[0] || '';
  const clipsTarUrl =
    status === 'completed' && videoUrl
      ? `https://mock.example.com/clips/${task.taskId}.tar`
      : '';
  const ext = {
    live_id: Number(task.sourceVideoId.replace(/\D/g, '')) || 0,
    live_name: task.sourceVideoName || '',
    video_project_id: numericId,
    sys_prompt_id: 0,
    target_duration_ms: 60000,
    draft_url: task.draftUrls[0] || '',
    video_url: videoUrl,
    clips_tar_url: clipsTarUrl,
  };

  return {
    id: String(task.taskId),
    type,
    status,
    progress: task.progress,
    sys_prompt: task.promptName || task.clipName,
    usr_prompt: task.taskType === 'ai_slice' || task.taskType === 'ai_slice_select' ? '请按高光片段剪辑' : '',
    video_project_name: task.clipName,
    live_name: task.sourceVideoName || '',
    live_url: task.m3u8Url || videoUrl || '',
    draft_url: task.draftUrls[0] || '',
    video_url: videoUrl,
    clips_tar_url: clipsTarUrl,
    width: 1080,
    height: 1920,
    created_by: '管理员',
    error_message: task.message ?? '',
    ext: JSON.stringify(ext),
    created_at: task.createdAt,
    started_at: task.createdAt,
    completed_at: status === 'completed' || status === 'failed' ? task.createdAt : '',
    updated_at: task.createdAt,
  };
}
