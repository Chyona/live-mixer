import type { NavigateFunction } from 'react-router-dom';

import { AUTH_TOKEN_KEY, emitAuthLogoutEvent } from '~/context/AuthContext';
import { isLoginPageMode } from '~/utils/config';
import { openLogin, type LoginFrom } from '~/utils/loginFlow';
import { navigateTo } from '~/utils/navigation';

export function clearAuthSession() {
  localStorage.removeItem(AUTH_TOKEN_KEY);
  emitAuthLogoutEvent();
}

let handlingSessionExpired = false;

type SessionExpiredNotifier = (message: string) => void;
let sessionExpiredNotifier: SessionExpiredNotifier | null = null;

/** 由 toast 模块注册，避免 authSession ↔ toast 循环依赖与动态导入告警 */
export function registerSessionExpiredNotifier(notifier: SessionExpiredNotifier | null) {
  sessionExpiredNotifier = notifier;
}

function currentFrom(): LoginFrom {
  return {
    pathname: window.location.pathname,
    search: window.location.search,
    hash: window.location.hash,
  };
}

function notifySessionExpired(message?: string) {
  const title = message?.trim() || '登录已过期';
  // 固定 key 防止并发请求叠多条
  sessionExpiredNotifier?.(title);
}

/**
 * 会话失效统一入口：提示、清本地凭证并进入登录。
 * HTTP 401 与业务码 12010/401 共用，避免延迟清会话造成的竞态与重复提示。
 */
export function handleSessionExpired(
  from?: LoginFrom,
  navigate?: NavigateFunction,
  options?: { message?: string }
) {
  if (handlingSessionExpired) return;
  handlingSessionExpired = true;

  const location = from ?? currentFrom();
  clearAuthSession();
  notifySessionExpired(options?.message);

  if (isLoginPageMode && location.pathname !== '/login') {
    const navOptions = {
      replace: true as const,
      state: { from: location },
    };
    if (navigate) {
      navigate('/login', navOptions);
    } else {
      navigateTo('/login', navOptions);
    }
  } else {
    openLogin(location);
  }

  window.setTimeout(() => {
    handlingSessionExpired = false;
  }, 3000);
}
