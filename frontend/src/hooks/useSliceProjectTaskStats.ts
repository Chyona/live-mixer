import { useCallback, useEffect, useRef, useState } from 'react';

import { AppError } from '~/services/http';
import { fetchSliceProjectRunningTasks } from '~/services/sliceProject';

const POLL_INTERVAL_MS = 5_000;

/**
 * 查询项目进行中/待进行任务数；数量 > 0 时切片页只读。
 * 无 projectId 时不请求、不锁定。每 5 秒轮询一次。
 */
export function useSliceProjectTaskStats(projectId: number | null | undefined) {
  const [runningTaskCount, setRunningTaskCount] = useState(0);
  const [loading, setLoading] = useState(false);
  const requestIdRef = useRef(0);

  const refresh = useCallback(async () => {
    if (projectId == null) {
      setRunningTaskCount(0);
      setLoading(false);
      return 0;
    }

    const requestId = ++requestIdRef.current;
    setLoading(true);
    try {
      const response = await fetchSliceProjectRunningTasks(projectId);
      if (requestId !== requestIdRef.current) return 0;
      const next = Math.max(0, Number(response.data?.total ?? 0) || 0);
      setRunningTaskCount(next);
      return next;
    } catch (error) {
      if (requestId !== requestIdRef.current) return 0;
      if (!(error instanceof AppError)) {
        console.error('加载项目进行中任务失败', error);
      }
      return 0;
    } finally {
      if (requestId === requestIdRef.current) {
        setLoading(false);
      }
    }
  }, [projectId]);

  useEffect(() => {
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
