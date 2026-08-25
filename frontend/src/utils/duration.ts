function normalizeSeconds(seconds: number): number {
  if (!Number.isFinite(seconds) || seconds < 0) {
    return 0;
  }

  return seconds;
}

export function formatVideoDuration(seconds: number): string {
  const totalSeconds = Math.floor(normalizeSeconds(seconds));
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const secs = totalSeconds % 60;

  if (hours > 0) {
    return `${hours}:${String(minutes).padStart(2, '0')}:${String(secs).padStart(2, '0')}`;
  }

  return `${minutes}:${String(secs).padStart(2, '0')}`;
}

/** 源视频等接口字段：时长单位为毫秒 */
export function formatVideoDurationMs(ms: number): string {
  return formatVideoDuration(ms / 1000);
}

export function formatMediaTime(seconds: number, withMs = false): string {
  const safeSeconds = normalizeSeconds(seconds);
  const totalSeconds = Math.floor(safeSeconds);
  const hours = Math.floor(totalSeconds / 3600);
  const minutes = Math.floor((totalSeconds % 3600) / 60);
  const secs = totalSeconds % 60;
  const ms = Math.floor((safeSeconds - totalSeconds) * 1000);

  const base =
    hours > 0
      ? `${hours}:${String(minutes).padStart(2, '0')}:${String(secs).padStart(2, '0')}`
      : `${minutes}:${String(secs).padStart(2, '0')}`;

  return withMs ? `${base}.${String(ms).padStart(3, '0')}` : base;
}
