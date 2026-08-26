import { useCallback, useEffect, useRef, useState } from 'react';

import { AppError } from '~/services/http';
import { fetchSliceProjectRunningTasks } from '~/services/sliceProject';

const POLL_INTERVAL_MS = 5_000;

export interface UseSliceProjectTaskStatsOptions {
  /** 进行中任务数从 >0 变为 0 时触发（在轮询回调内同步判断） */
  onTasksCompleted?: () => void;
}

/**
 * 查询项目进行中/待进行任务数；数量 > 0 时切片页只读。
 * 无 projectId 时不请求、不锁定。每 5 秒轮询一次。
 */
export function useSliceProjectTaskStats(
  projectId: number | null | undefined,
  options?: UseSliceProjectTaskStatsOptions
) {
  const [runningTaskCount, setRunningTaskCount] = useState(0);
  const [loading, setLoading] = useState(false);
  const requestIdRef = useRef(0);
  const prevCountRef = useRef(0);
  const onTasksCompletedRef = useRef(options?.onTasksCompleted);
  onTasksCompletedRef.current = options?.onTasksCompleted;

  const notifyIfTasksCompleted = useCallback((prev: number, next: number) => {
    if (projectId == null) return;
    if (prev > 0 && next === 0) {
      onTasksCompletedRef.current?.();
    }
  }, [projectId]);

  const refresh = useCallback(async () => {
    if (projectId == null) {
      prevCountRef.current = 0;
      setRunningTaskCount(0);
      setLoading(false);
      return 0;
    }

    const requestId = ++requestIdRef.current;
    setLoading(true);
    try {
      const response = await fetchSliceProjectRunningTasks(projectId);
      if (requestId !== requestIdRef.current) return prevCountRef.current;
      const next = Math.max(0, Number(response.data?.total ?? 0) || 0);
      const prev = prevCountRef.current;
      prevCountRef.current = next;
      setRunningTaskCount(next);
      notifyIfTasksCompleted(prev, next);
      return next;
    } catch (error) {
      if (requestId !== requestIdRef.current) return prevCountRef.current;
      if (!(error instanceof AppError)) {
        console.error('加载项目进行中任务失败', error);
      }
      return prevCountRef.current;
    } finally {
      if (requestId === requestIdRef.current) {
        setLoading(false);
      }
    }
  }, [notifyIfTasksCompleted, projectId]);

  useEffect(() => {
    prevCountRef.current = 0;
    if (projectId == null) {
      setRunningTaskCount(0);
      setLoading(false);
      return;
    }

    void refresh();
    const timer = window.setInterval(() => {
      void refresh();
    }, POLL_INTERVAL_MS);

    return () => {
      window.clearInterval(timer);
      requestIdRef.current += 1;
    };
  }, [projectId, refresh]);

  const readOnly = projectId != null && runningTaskCount > 0;

  return {
    runningTaskCount,
    readOnly,
    loading,
    refresh,
  };
}
