/** 业务状态码，按后端约定在此扩展。code === 0 成功，其余均为异常。 */
export const BusinessCode = {
  SUCCESS: 0,
  /** 兼容部分接口用 HTTP 语义码表示会话失效 */
  UNAUTHORIZED: 401,
  SESSION_EXPIRED: 12010,
} as const;

export type BusinessCodeValue = (typeof BusinessCode)[keyof typeof BusinessCode];

export function isBusinessSuccess(code: number): boolean {
  return code === BusinessCode.SUCCESS;
}

/** 业务体 code / HTTP status：token 无效或会话过期 */
export function isSessionExpiredCode(code: unknown): boolean {
  const n = typeof code === 'number' ? code : Number(code);
  return n === BusinessCode.UNAUTHORIZED || n === BusinessCode.SESSION_EXPIRED;
}
