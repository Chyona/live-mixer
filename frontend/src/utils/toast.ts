import { message as staticMessage, notification as staticNotification } from 'antd';
import type { MessageInstance } from 'antd/es/message/interface';
import type { NotificationInstance } from 'antd/es/notification/interface';
import type { ReactNode } from 'react';
import { registerSessionExpiredNotifier } from '~/services/authSession';
import { AppError, isUnauthorizedError } from '~/services/http';

type ToastApis = {
  message: MessageInstance;
  notification: NotificationInstance;
};

let apis: ToastApis | null = null;

/** 由 ToastProvider 注册，使 toast 能消费 ConfigProvider 主题 */
export function registerToast(instance: ToastApis | null) {
  apis = instance;
}

function msg() {
  return apis?.message ?? staticMessage;
}

function notify() {
  return apis?.notification ?? staticNotification;
}

const DEFAULT_DURATION = 3;
const NOTIFY_PLACEMENT = 'topRight' as const;

export interface ToastNotifyOptions {
  title: string;
  description?: string;
  duration?: number;
  /** 显示自动关闭倒计时进度条（按类型配色），默认 true */
  showProgress?: boolean;
  pauseOnHover?: boolean;
  key?: string;
  type?: 'success' | 'error' | 'info' | 'warning';
  /** 通知底部操作区（如「查看」按钮）；antd 5.22 使用 btn */
  btn?: ReactNode;
}

type NotifyExtra = Partial<
  Pick<ToastNotifyOptions, 'duration' | 'showProgress' | 'pauseOnHover' | 'key' | 'btn'>
>;

function openNotify(
  type: 'success' | 'error' | 'info' | 'warning',
  options: ToastNotifyOptions
) {
  const {
    title,
    description,
    duration = DEFAULT_DURATION,
    showProgress = true,
    pauseOnHover = true,
    key,
    btn,
  } = options;

  // antd 原生 progress 固定主色，难以按类型改色；改用自绘底部进度条
  const withProgress = showProgress && duration > 0;

  notify()[type]({
    key,
    message: title,
    description,
    btn,
    placement: NOTIFY_PLACEMENT,
    duration,
    showProgress: false,
    pauseOnHover,
    className: [
      'toast-notify',
      `toast-notify-${type}`,
      withProgress ? 'toast-notify--progress' : '',
    ]
      .filter(Boolean)
      .join(' '),
    style: withProgress
      ? ({
          ['--toast-duration' as string]: `${duration}s`,
        } as Record<string, string>)
      : undefined,
  });
}

function notifyMethod(type: 'success' | 'error' | 'info' | 'warning') {
  return (title: string, description?: string, extra?: NotifyExtra) => {
    openNotify(type, { title, description, ...extra });
  };
}

/** 全局轻提示（message 为短提示，notify 为右上角通知并默认带按类型配色的倒计时条） */
export const toast = {
  success(content: string, duration = DEFAULT_DURATION) {
    msg().success(content, duration);
  },
  error(content: string, duration = DEFAULT_DURATION) {
    msg().error(content, duration);
  },
  info(content: string, duration = DEFAULT_DURATION) {
    msg().info(content, duration);
  },
  warning(content: string, duration = DEFAULT_DURATION) {
    msg().warning(content, duration);
  },

  notify: {
    success: notifyMethod('success'),
    error: notifyMethod('error'),
    info: notifyMethod('info'),
    warning: notifyMethod('warning'),

    open(options: ToastNotifyOptions) {
      const { type = 'info', ...rest } = options;
      openNotify(type, rest);
    },

    destroy(key?: string) {
      notify().destroy(key);
    },
  },
};

/** 展示 HTTP / 业务层 AppError */
const ERROR_DEDUPE_MS = 3000;
const recentErrorKeys = new Map<string, number>();

function shouldShowError(key: string): boolean {
  const now = Date.now();
  const lastShownAt = recentErrorKeys.get(key);
  if (lastShownAt != null && now - lastShownAt < ERROR_DEDUPE_MS) {
    return false;
  }
  recentErrorKeys.set(key, now);
  return true;
}

/** 同一 scope 短时间内只展示一条错误，避免重复请求叠加提示 */
export function showScopedError(scope: string, title: string) {
  const message = title.trim();
  if (!message || !shouldShowError(scope)) return;
  toast.notify.error(message);
}

export function showAppError(error: AppError, scope?: string) {
  if (isUnauthorizedError(error)) return;
  const key = scope ?? `msg:${error.errorMessage}`;
  if (!shouldShowError(key)) return;
  toast.notify.error(error.errorMessage);
}

/** 列表/页面加载等场景的统一错误处理，优先展示接口错误信息 */
export function handleRequestError(scope: string, error: unknown, fallbackMessage: string) {
  if (error instanceof AppError) {
    if (isUnauthorizedError(error)) return;
    showScopedError(scope, error.errorMessage || fallbackMessage);
    return;
  }
  showScopedError(scope, fallbackMessage);
}

registerSessionExpiredNotifier((title) => {
  toast.notify.warning(title, undefined, { key: 'session-expired' });
});
