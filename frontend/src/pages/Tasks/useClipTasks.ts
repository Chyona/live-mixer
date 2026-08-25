import { useCallback, useEffect, useRef, useState } from 'react';

import {
  fetchClipTaskList,
  type ClipTaskItem,
  type ClipTaskListParams,
} from '~/services/task';
import { isClipTaskActive } from './utils';

/** 进行中任务自动刷新间隔（秒） */
export const CLIP_TASK_POLL_INTERVAL_SEC = 5;
const POLL_INTERVAL_MS = CLIP_TASK_POLL_INTERVAL_SEC * 1000;

export function useClipTasks(filters: ClipTaskListParams = {}) {
  const [tasks, setTasks] = useState<ClipTaskItem[]>([]);
  const [total, setTotal] = useState(0);
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const [polling, setPolling] = useState(false);
  const loadingRef = useRef(false);
  const tasksRef = useRef<ClipTaskItem[]>([]);
  const filtersRef = useRef(filters);

  useEffect(() => {
    tasksRef.current = tasks;
  }, [tasks]);

  useEffect(() => {
    filtersRef.current = filters;
  }, [filters]);

  const applyTaskList = useCallback((list: ClipTaskItem[], nextTotal: number) => {
    tasksRef.current = list;
    setTasks(list);
    setTotal(nextTotal);
  }, []);

  const refreshTasks = useCallback(
    async (options?: { showLoading?: boolean; refresh?: boolean; silent?: boolean }) => {
      if (loadingRef.current) return;

      const { showLoading = false, refresh = false, silent = false } = options ?? {};
      loadingRef.current = true;

      if (refresh) {
        setRefreshing(true);
      } else if (showLoading) {
        setLoading(true);
      } else if (silent) {
        setPolling(true);
      }

      try {
        const response = await fetchClipTaskList(filtersRef.current);
        if (response.code !== 0) return;
        applyTaskList(response.data.list, response.data.total);
      } finally {
        loadingRef.current = false;
        if (refresh) {
          setRefreshing(false);
        } else if (showLoading) {
          setLoading(false);
        } else if (silent) {
          setPolling(false);
        }
      }
    },
    [applyTaskList]
  );

  const reload = useCallback(async () => {
    await refreshTasks({ refresh: true });
  }, [refreshTasks]);

  useEffect(() => {
    void refreshTasks({
      showLoading: true,
    });
  }, [
    filters.type,
    filters.status,
    filters.start_date,
    filters.end_date,
    filters.keywords,
    filters.page,
    filters.page_size,
    refreshTasks,
  ]);

  useEffect(() => {
    const timer = window.setInterval(() => {
      const hasActive = tasksRef.current.some((task) => isClipTaskActive(task.status));
      if (!hasActive) return;
      void refreshTasks({ silent: true });
    }, POLL_INTERVAL_MS);

    return () => window.clearInterval(timer);
  }, [refreshTasks]);

  const hasActiveTasks = tasks.some((task) => isClipTaskActive(task.status));

  return {
    tasks,
    total,
    loading,
    refreshing,
    polling,
    hasActiveTasks,
    reload,
  };
}
