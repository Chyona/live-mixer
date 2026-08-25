import type { NavigateFunction } from 'react-router-dom';

// 全局导航解决方案
let globalNavigate: NavigateFunction | null = null;

export const setGlobalNavigate = (navigate: NavigateFunction | null) => {
  globalNavigate = navigate;
};

export type NavigateToOptions = {
  replace?: boolean;
  state?: unknown;
};

export const navigateTo = (path: string, options?: NavigateToOptions) => {
  if (globalNavigate) {
    globalNavigate(path, {
      replace: options?.replace,
      state: options?.state,
    });
  } else {
    console.warn('导航函数尚未初始化，使用原生导航');
    window.location.assign(path);
  }
};
