import type { Location } from 'react-router-dom';

import { getLoginModalStore } from '~/components/LoginModal/store';
import { isLoginModalMode, isLoginPageMode } from '~/utils/config';
import { DEFAULT_APP_PATH } from '~/routes/const';
import { navigateTo } from '~/utils/navigation';

type LoginFrom = Pick<Location, 'pathname' | 'search' | 'hash'>;

/**
 * 仅允许站内相对路径：以单个 `/` 开头。
 * 拒绝 `//evil.com`、带 scheme、反斜杠等开放重定向载荷。
 */
export function isSafeInternalPath(path: string): boolean {
  if (!path || typeof path !== 'string') return false;
  if (!path.startsWith('/') || path.startsWith('//')) return false;
  if (path.includes('\\')) return false;

  try {
    const decoded = decodeURIComponent(path);
    if (decoded.startsWith('//') || decoded.includes('\\')) return false;
    if (/^[a-zA-Z][a-zA-Z0-9+.-]*:/.test(decoded)) return false;
  } catch {
    return false;
  }

  return true;
}

/** 不安全回跳时回落到默认应用路径 */
export function sanitizeReturnPath(path: string | null | undefined, fallback = DEFAULT_APP_PATH): string {
  if (path && isSafeInternalPath(path)) return path;
  return fallback;
}

function buildReturnPath(from?: LoginFrom) {
  const pathname = from?.pathname ?? window.location.pathname;
  const search = from?.search ?? window.location.search;
  const hash = from?.hash ?? window.location.hash;
  return sanitizeReturnPath(`${pathname}${search}${hash}`);
}

export { buildReturnPath };
export type { LoginFrom };

/** 按 VITE_LOGIN_MODE 打开登录页或登录弹窗 */
export function openLogin(from?: LoginFrom) {
  if (isLoginModalMode) {
    const returnTo = buildReturnPath(from);
    const store = getLoginModalStore();
    store.returnTo = returnTo === '/login' ? DEFAULT_APP_PATH : returnTo;
    store.open = true;
    return;
  }

  const pathname = from?.pathname ?? window.location.pathname;
  if (pathname === '/login') {
    getLoginModalStore().open = true;
    return;
  }

  const pathnameSafe = isSafeInternalPath(pathname);
  navigateTo('/login', {
    state: {
      from: {
        pathname: pathnameSafe ? pathname : DEFAULT_APP_PATH,
        search: pathnameSafe ? (from?.search ?? '') : '',
        hash: pathnameSafe ? (from?.hash ?? '') : '',
      },
    },
  });
}

export function closeLogin() {
  const store = getLoginModalStore();
  store.open = false;
  store.returnTo = null;
}

/** Modal 模式登录成功后跳回来源页 */
export function completeLoginRedirect() {
  if (!isLoginModalMode) return;

  const store = getLoginModalStore();
  const returnTo = sanitizeReturnPath(store.returnTo || DEFAULT_APP_PATH);
  store.open = false;
  store.returnTo = null;

  const current = `${window.location.pathname}${window.location.search}${window.location.hash}`;
  if (returnTo !== current) {
    navigateTo(returnTo);
  }
}

export { isLoginPageMode, isLoginModalMode };
