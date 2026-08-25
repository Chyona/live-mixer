import type { MockMethod } from 'vite-plugin-mock';
import { API_PREFIX } from './_config';
import { createClipTask, getClipTaskPollResult } from './clipTaskStore';

export default [
  {
    url: `${API_PREFIX}/clipflow-ai/v2/video/process`,
    method: 'post',
    response: ({
      body,
    }: {
      body: {
        project_id?: string | number;
      };
    }) => {
      const projectId = body?.project_id;
      if (projectId == null || String(projectId).trim() === '') {
        return { code: 400, message: '请提供项目 ID', data: null };
      }

      const task_id = `clip-task-${Date.now()}`;
      createClipTask({
        taskId: task_id,
        sourceVideoId: String(projectId),
        sourceVideoName: `项目 ${projectId}`,
        m3u8Url: '',
      });

      return {
        code: 0,
        message: '',
        data: { task_id },
      };
    },
  },
  {
    url: `${API_PREFIX}/clipflow-ai/v1/task`,
    method: 'get',
    response: ({ query }: { query: { task_id?: string } }) => {
      const taskId = typeof query.task_id === 'string' ? query.task_id : '';
      const result = getClipTaskPollResult(taskId);

      if (!result) {
        return { code: 404, message: '剪辑任务不存在', data: null };
      }

      return {
        code: 0,
        message: '',
        data: result,
      };
    },
  },
] as MockMethod[];
