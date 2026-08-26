export type AsrParagraphsApiKey = 'asr_paragraphs' | 'live_asr';

const URL_PARAM = 'isdebug';
const DEFAULT_KEY: AsrParagraphsApiKey = 'asr_paragraphs';
const DEBUG_KEY: AsrParagraphsApiKey = 'live_asr';

let loggedUrlOverride = false;
/** 本次会话内 isdebug 是否应随路由保留（见 syncDebugSearchPersistFromUrl） */
let debugSearchPersist = false;

function isDebugModeFromSearch(search: string | URLSearchParams): boolean {
  const params = search instanceof URLSearchParams ? search : new URLSearchParams(search);
  return params.get(URL_PARAM)?.toLowerCase() === 'true';
}

/** 根据 URL 同步 isdebug  sticky 状态：true 开启；false 关闭 */
export function syncDebugSearchPersistFromUrl(search: string | URLSearchParams): void {
  debugSafe(() => {
    const params = search instanceof URLSearchParams ? search : new URLSearchParams(search);
    const value = params.get(URL_PARAM)?.toLowerCase();
    if (value === 'true') {
      debugSearchPersist = true;
      return;
    }
    if (value === 'false') {
      debugSearchPersist = false;
    }
  }, undefined);
}

export function isDebugSearchPersistEnabled(): boolean {
  return debugSafe(() => {
    if (typeof window !== 'undefined' && isDebugModeFromSearch(window.location.search)) {
      return true;
    }
    return debugSearchPersist;
  }, false);
}

/** 仅测试用：重置 sticky 状态 */
export function resetDebugSearchPersistForTests(): void {
  debugSearchPersist = false;
  loggedUrlOverride = false;
}

function debugSafe<T>(action: () => T, fallback: T): T {
  try {
    return action();
  } catch (error) {
    console.warn('[isdebug] 调试逻辑异常，已降级不影响页面', error);
    return fallback;
  }
}

function readAsrKeyFromCurrentUrl(): AsrParagraphsApiKey | null {
  return debugSafe(() => {
    if (typeof window === 'undefined') return null;
    return isDebugModeFromSearch(window.location.search) ? DEBUG_KEY : null;
  }, null);
}

/** 当前应读取的源视频 ASR 文案接口字段（默认 asr_paragraphs，URL ?isdebug=true 读 live_asr） */
export function getAsrParagraphsApiKey(): AsrParagraphsApiKey {
  return debugSafe(() => {
    const urlOverride = readAsrKeyFromCurrentUrl();
    if (urlOverride && !loggedUrlOverride && typeof window !== 'undefined') {
      loggedUrlOverride = true;
      console.info('[debug] ASR 文案接口字段:', urlOverride);
    }
    return urlOverride ?? DEFAULT_KEY;
  }, DEFAULT_KEY);
}

export function isAsrParagraphsApiKeyOverridden(): boolean {
  return debugSafe(() => readAsrKeyFromCurrentUrl() != null, false);
}

/** 路由跳转时把 isdebug 合并进目标 query，避免换页丢失 */
export function mergeDebugAsrKeySearchParams(search: URLSearchParams): URLSearchParams {
  return debugSafe(() => {
    const next = new URLSearchParams(search);
    if (next.has(URL_PARAM)) return next;

    if (isDebugSearchPersistEnabled()) {
      next.set(URL_PARAM, 'true');
    }
    return next;
  }, new URLSearchParams(search));
}

export function appendDebugAsrKeyToPath(path: string): string {
  return debugSafe(() => {
    const qIndex = path.indexOf('?');
    const pathname = qIndex >= 0 ? path.slice(0, qIndex) : path;
    const existing =
      qIndex >= 0 ? new URLSearchParams(path.slice(qIndex + 1)) : new URLSearchParams();
    const merged = mergeDebugAsrKeySearchParams(existing);
    const str = merged.toString();
    return str ? `${pathname}?${str}` : pathname;
  }, path);
}
