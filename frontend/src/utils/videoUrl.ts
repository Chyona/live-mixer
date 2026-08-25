const TOS_HOST_SUFFIX = '.volces.com';

const HLS_PATTERN = /\.m3u8(\?|$)/i;
const NATIVE_VIDEO_PATTERN = /\.(mp4|webm|ogg|mov|m4v|mkv|avi|flv)(\?|$)/i;

export type VideoSourceType = 'hls' | 'native' | 'unsupported';

export function detectVideoSourceType(url: string): VideoSourceType {
  const trimmed = url.trim();
  if (!trimmed) return 'unsupported';

  if (HLS_PATTERN.test(trimmed)) {
    return 'hls';
  }

  if (NATIVE_VIDEO_PATTERN.test(trimmed)) {
    return 'native';
  }

  if (/^https?:\/\//i.test(trimmed)) {
    return 'native';
  }

  return 'unsupported';
}

export function isPlayableVideoUrl(url: string): boolean {
  return detectVideoSourceType(url) !== 'unsupported';
}

export function getVideoFormatLabel(url: string): string {
  const type = detectVideoSourceType(url);
  if (type === 'hls') return 'HLS (m3u8)';
  if (type === 'unsupported') return '不支持';

  const match = url.trim().match(/\.(\w+)(\?|$)/i);
  return match?.[1]?.toUpperCase() ?? '视频文件';
}

export function resolveVideoPlayUrl(url: string): string {
  const trimmed = url.trim();
  if (!trimmed) return '';

  if (!import.meta.env.DEV) {
    return trimmed;
  }

  try {
    const parsed = new URL(trimmed);
    if (parsed.hostname.endsWith(TOS_HOST_SUFFIX)) {
      return `/tos-media${parsed.pathname}${parsed.search}`;
    }
  } catch {
    return trimmed;
  }

  return trimmed;
}

export function getVideoErrorMessage(error: MediaError | null | undefined): string {
  if (!error) {
    return '视频加载失败，请检查播放地址是否有效';
  }

  switch (error.code) {
    case MediaError.MEDIA_ERR_ABORTED:
      return '视频加载已中断';
    case MediaError.MEDIA_ERR_NETWORK:
      return '视频网络加载失败，请检查网络或播放地址是否过期';
    case MediaError.MEDIA_ERR_DECODE:
      return '视频解码失败，文件可能已损坏';
    case MediaError.MEDIA_ERR_SRC_NOT_SUPPORTED:
      return '无法播放该视频，播放地址可能已过期或格式不受支持';
    default:
      return '视频加载失败，请检查播放地址是否有效';
  }
}

export function getUnsupportedVideoMessage(url: string): string {
  const type = detectVideoSourceType(url);
  if (type !== 'unsupported') return '';

  return '当前播放地址格式不受支持，请使用 m3u8、mp4 等浏览器可播放的视频链接';
}

/** @deprecated 使用 isPlayableVideoUrl */
export function isPlayableStreamUrl(url: string): boolean {
  return isPlayableVideoUrl(url);
}
