import axios from 'axios';

import { apiPath } from '~/utils/api';

import { AppError, DEFAULT_REQUEST_TIMEOUT_MS } from './http';

/** 从 Content-Disposition 解析后端返回的文件名（优先 RFC 5987 的 filename*）。 */
export function parseContentDispositionFilename(header?: string): string | null {
  if (!header) return null;

  const utf8Match = /filename\*=UTF-8''([^;]+)/i.exec(header);
  if (utf8Match?.[1]) {
    try {
      return decodeURIComponent(utf8Match[1].trim());
    } catch {
      return utf8Match[1].trim();
    }
  }

  const plainMatch = /filename="?([^";]+)"?/i.exec(header);
  return plainMatch?.[1]?.trim() || null;
}

/** 去除文件名中的非法字符。 */
export function sanitizeDownloadFilename(name: string): string {
  return name.replace(/[\\/:*?"<>|]/g, '_').trim() || 'subtitle';
}

/** 触发浏览器下载。 */
export function triggerBlobDownload(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename;
  anchor.click();
  URL.revokeObjectURL(url);
}

/** 从 blob 错误响应中解析后端 message。 */
export async function resolveBlobErrorMessage(error: unknown, fallback: string): Promise<string> {
  if (!(error instanceof AppError)) {
    return fallback;
  }

  const data = error.resp?.response?.data;
  if (data instanceof Blob) {
    try {
      const text = await data.text();
      const payload = JSON.parse(text) as { message?: string; errorMessage?: string };
      const message =
        (typeof payload.message === 'string' && payload.message.trim()) ||
        (typeof payload.errorMessage === 'string' && payload.errorMessage.trim()) ||
        '';
      if (message) return message;
    } catch {
      // ignore parse failure
    }
  }

  return error.errorMessage || fallback;
}

export interface DownloadBlobOptions {
  url: string;
  method: 'get' | 'post';
  data?: unknown;
  /** 后端未返回 Content-Disposition 时使用的文件名 */
  fallbackFilename: string;
  /** 无法解析后端错误信息时的兜底提示 */
  fallbackError: string;
  /** 响应体为空时的提示；缺省使用 fallbackError */
  emptyMessage?: string;
}

/** 请求二进制文件并触发下载；统一处理后端 JSON 错误与空响应。 */
export async function downloadBlob(options: DownloadBlobOptions): Promise<void> {
  const { url, method, data, fallbackFilename, fallbackError } = options;
  const emptyMessage = options.emptyMessage ?? fallbackError;

  try {
    const response = await axios.request<Blob>({
      url: apiPath(url),
      method,
      data,
      responseType: 'blob',
      timeout: DEFAULT_REQUEST_TIMEOUT_MS,
    });

    const blob = response.data;
    if (!blob || blob.size === 0) {
      throw new Error(emptyMessage);
    }

    const filename =
      parseContentDispositionFilename(response.headers['content-disposition']) || fallbackFilename;

    triggerBlobDownload(blob, filename);
  } catch (error) {
    if (error instanceof Error && error.message === emptyMessage) {
      throw error;
    }

    const message = await resolveBlobErrorMessage(error, fallbackError);
    if (error instanceof AppError) {
      throw new AppError(message, error.errorCode, error.resp);
    }
    throw new Error(message);
  }
}
