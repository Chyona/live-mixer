import { describe, expect, it, vi, beforeEach } from 'vitest';
import { AppError } from '~/services/http';
import { handleRequestError, registerToast, showAppError, showScopedError, toast } from './toast';

describe('toast', () => {
  beforeEach(() => {
    registerToast(null);
  });

  it('calls registered message API', () => {
    const success = vi.fn();
    registerToast({
      message: { success, error: vi.fn(), info: vi.fn(), warning: vi.fn() } as never,
      notification: {
        success: vi.fn(),
        error: vi.fn(),
        info: vi.fn(),
        warning: vi.fn(),
        open: vi.fn(),
        destroy: vi.fn(),
      } as never,
    });

    toast.success('ok');
    expect(success).toHaveBeenCalledWith('ok', 3);
  });

  it('enables showProgress by default on notify', () => {
    const success = vi.fn();
    registerToast({
      message: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() } as never,
      notification: {
        success,
        error: vi.fn(),
        info: vi.fn(),
        warning: vi.fn(),
        open: vi.fn(),
        destroy: vi.fn(),
      } as never,
    });

    toast.notify.success('完成', '操作成功');
    expect(success).toHaveBeenCalledWith(
      expect.objectContaining({
        message: '完成',
        description: '操作成功',
        showProgress: false,
        duration: 3,
        pauseOnHover: true,
        className: 'toast-notify toast-notify-success toast-notify--progress',
        style: { '--toast-duration': '3s' },
      })
    );
  });

  it('can disable showProgress', () => {
    const error = vi.fn();
    registerToast({
      message: { success: vi.fn(), error, info: vi.fn(), warning: vi.fn() } as never,
      notification: {
        success: vi.fn(),
        error,
        info: vi.fn(),
        warning: vi.fn(),
        open: vi.fn(),
        destroy: vi.fn(),
      } as never,
    });

    toast.notify.error('失败', undefined, { showProgress: false });
    expect(error).toHaveBeenCalledWith(
      expect.objectContaining({
        showProgress: false,
        className: 'toast-notify toast-notify-error',
      })
    );
  });

  it('dedupes scoped errors within a short window', () => {
    const error = vi.fn();
    registerToast({
      message: { success: vi.fn(), error, info: vi.fn(), warning: vi.fn() } as never,
      notification: {
        success: vi.fn(),
        error,
        info: vi.fn(),
        warning: vi.fn(),
        open: vi.fn(),
        destroy: vi.fn(),
      } as never,
    });

    showScopedError('slices-list', '加载剪辑项目失败');
    showScopedError('slices-list', '请求未收到响应！');

    expect(error).toHaveBeenCalledTimes(1);
    expect(error).toHaveBeenCalledWith(
      expect.objectContaining({ message: '加载剪辑项目失败' })
    );
  });

  it('prefers app error message in handleRequestError', () => {
    const error = vi.fn();
    registerToast({
      message: { success: vi.fn(), error, info: vi.fn(), warning: vi.fn() } as never,
      notification: {
        success: vi.fn(),
        error,
        info: vi.fn(),
        warning: vi.fn(),
        open: vi.fn(),
        destroy: vi.fn(),
      } as never,
    });

    handleRequestError(
      'slices-list-2',
      new AppError('请求未收到响应！', 500, {} as never),
      '加载剪辑项目失败'
    );

    expect(error).toHaveBeenCalledWith(
      expect.objectContaining({ message: '请求未收到响应！' })
    );
  });

  it('dedupes repeated app errors by message when no scope is provided', () => {
    const error = vi.fn();
    registerToast({
      message: { success: vi.fn(), error, info: vi.fn(), warning: vi.fn() } as never,
      notification: {
        success: vi.fn(),
        error,
        info: vi.fn(),
        warning: vi.fn(),
        open: vi.fn(),
        destroy: vi.fn(),
      } as never,
    });

    const appError = new AppError('请求未收到响应！', 500, {} as never);
    showAppError(appError);
    showAppError(appError);

    expect(error).toHaveBeenCalledTimes(1);
  });

  it('silences session-expired app errors', () => {
    const error = vi.fn();
    registerToast({
      message: { success: vi.fn(), error, info: vi.fn(), warning: vi.fn() } as never,
      notification: {
        success: vi.fn(),
        error,
        info: vi.fn(),
        warning: vi.fn(),
        open: vi.fn(),
        destroy: vi.fn(),
      } as never,
    });

    showAppError(new AppError('未登录或登录已过期', 12010));
    handleRequestError('list', new AppError('未登录或登录已过期', 401), '加载失败');

    expect(error).not.toHaveBeenCalled();
  });

  it('supports notify btn and destroy', () => {
    const warning = vi.fn();
    const destroy = vi.fn();
    registerToast({
      message: { success: vi.fn(), error: vi.fn(), info: vi.fn(), warning: vi.fn() } as never,
      notification: {
        success: vi.fn(),
        error: vi.fn(),
        info: vi.fn(),
        warning,
        open: vi.fn(),
        destroy,
      } as never,
    });

    const btn = '查看';
    toast.notify.warning('源视频已存在', '可点击查看', { key: 'dup', btn });
    expect(warning).toHaveBeenCalledWith(
      expect.objectContaining({
        message: '源视频已存在',
        description: '可点击查看',
        key: 'dup',
        btn,
      })
    );

    toast.notify.destroy('dup');
    expect(destroy).toHaveBeenCalledWith('dup');
  });
});
