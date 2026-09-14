/** 把视频定位到指定秒。片尾 ended 时部分浏览器会忽略 currentTime，需在同一调用里 play 再补一次。 */

export function resolveSeekTarget(time: number, duration: number): number | null {
  if (!Number.isFinite(time)) return null;

  const target = Math.max(0, time);
  if (!Number.isFinite(duration) || duration <= 0) return target;

  return Math.min(target, Math.max(duration - 0.04, 0));
}

export function seekHtmlVideo(video: HTMLVideoElement, time: number): number {
  const target = resolveSeekTarget(time, video.duration);
  if (target == null) return video.currentTime || 0;

  const apply = () => {
    try {
      video.currentTime = target;
    } catch {
      // ignore seek errors on unloaded media
    }
  };

  apply();

  if (video.paused || video.ended) {
    void video
      .play()
      .then(() => {
        if (Math.abs((video.currentTime || 0) - target) > 0.4) {
          apply();
        }
      })
      .catch(() => undefined);
  }

  return target;
}
