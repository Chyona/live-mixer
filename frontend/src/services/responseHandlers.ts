import type { BaseResponse } from './types';

/**
 * 处理需全局拦截的业务码（与 HTTP 传输层解耦）。
 * SESSION_EXPIRED(12010) 在 request() 中处理并 throw，不在此重复。
 */
export function handleBusinessResponse(_response: BaseResponse): void {
  // 其他全局业务码可在此扩展
}
