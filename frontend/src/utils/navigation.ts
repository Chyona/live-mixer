import type { NavigateFunction } from 'react-router-dom';
import { appendDebugAsrKeyToPath } from '~/utils/asrParagraphsKey';

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
  let target = path;
  try {
    target = appendDebugAsrKeyToPath(path);
  } catch (error) {
    console.warn('[isdebug] 导航参数合并失败，已使用原路径', error);
  }
  if (globalNavigate) {
    globalNavigate(target, {
      replace: options?.replace,
      state: options?.state,
    });
  } else {
    console.warn('导航函数尚未初始化，使用原生导航');
    window.location.assign(target);
  }
};
