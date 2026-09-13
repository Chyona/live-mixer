/** 预览兜底：跟播 master 等 copy 封装在浏览器里易出现音画分家，seek 可把 A/V 重新对齐。 */

export const AV_DRIFT_THRESHOLD_SEC = 0.35;
export const AV_DRIFT_CHECK_INTERVAL_MS = 250;
export const AV_DRIFT_COOLDOWN_MS = 1500;
/** 播放中媒体时钟几乎不动、墙钟仍前进，视为画面卡住 */
export const AV_FREEZE_WALL_SEC = 0.35;
export const AV_FREEZE_MEDIA_SEC = 0.05;

export type AvDriftResyncOptions = {
  thresholdSec?: number;
  checkIntervalMs?: number;
  cooldownMs?: number;
  onResync?: (info: { driftSec: number; reason: AvDriftResyncReason }) => void;
};

export type AvDriftResyncReason = 'clock_drift' | 'waiting' | 'freeze';

export type AvDriftResyncHandle = {
  destroy: () => void;
};

export function shouldCorrectAvDrift(driftSec: number, thresholdSec = AV_DRIFT_THRESHOLD_SEC): boolean {
  return Number.isFinite(driftSec) && Math.abs(driftSec) >= thresholdSec;
}

export function detectPlaybackFreeze(params: {
  paused: boolean;
  seeking: boolean;
  wallDeltaSec: number;
  mediaDeltaSec: number;
  freezeWallSec?: number;
  freezeMediaSec?: number;
}): boolean {
  const {
    paused,
    seeking,
    wallDeltaSec,
    mediaDeltaSec,
    freezeWallSec = AV_FREEZE_WALL_SEC,
    freezeMediaSec = AV_FREEZE_MEDIA_SEC,
  } = params;
  if (paused || seeking) return false;
  if (!(wallDeltaSec >= freezeWallSec)) return false;
  return mediaDeltaSec < freezeMediaSec;
}

function resolveMediaUrl(video: HTMLVideoElement): string {
  return (video.currentSrc || video.src || '').trim();
}

function softSeekMedia(media: HTMLMediaElement, time: number) {
  if (!Number.isFinite(time) || time < 0) return;
  try {
    media.currentTime = time;
  } catch {
    // ignore seek errors on unloaded media
  }
}

/**
 * 用静音 audio 跟播同一地址作音频钟；与 video.currentTime 偏差超阈值时对 video soft-seek 纠偏。
 * 仅适合原生文件（mp4 等）；HLS 需两套 loader，此处不启用。
 */
export function attachAvDriftResync(
  video: HTMLVideoElement,
  options: AvDriftResyncOptions = {}
): AvDriftResyncHandle {
  const thresholdSec = options.thresholdSec ?? AV_DRIFT_THRESHOLD_SEC;
  const checkIntervalMs = options.checkIntervalMs ?? AV_DRIFT_CHECK_INTERVAL_MS;
  const cooldownMs = options.cooldownMs ?? AV_DRIFT_COOLDOWN_MS;
  const onResync = options.onResync;

  const audio = document.createElement('audio');
  audio.preload = 'auto';
  audio.muted = true;
  audio.volume = 0;
  audio.setAttribute('aria-hidden', 'true');
  audio.style.display = 'none';
  document.body.appendChild(audio);

  let destroyed = false;
  let lastCorrectAt = 0;
  let pendingWaitingResync = false;
  let sawFreeze = false;
  let lastVideoTime = video.currentTime || 0;
  let lastWallMs = performance.now();
  let syncingAudio = false;

  const syncAudioFromVideo = (force = false) => {
    if (destroyed) return;
    const url = resolveMediaUrl(video);
    if (!url) return;

    if (audio.crossOrigin !== video.crossOrigin) {
      if (video.crossOrigin) {
        audio.crossOrigin = video.crossOrigin;
      } else {
        audio.removeAttribute('crossorigin');
      }
    }

    const audioUrl = (audio.currentSrc || audio.src || '').trim();
    if (audioUrl !== url) {
      audio.src = url;
      audio.load();
    }

    const target = video.currentTime || 0;
    if (force || Math.abs((audio.currentTime || 0) - target) > 0.05) {
      syncingAudio = true;
      softSeekMedia(audio, target);
      window.setTimeout(() => {
        syncingAudio = false;
      }, 0);
    }

    audio.playbackRate = video.playbackRate || 1;
  };

  const correct = (reason: AvDriftResyncReason, driftSec: number) => {
    if (destroyed) return;
    const now = performance.now();
    if (now - lastCorrectAt < cooldownMs) return;
    if (video.seeking) return;

    lastCorrectAt = now;
    const t = video.currentTime || 0;
    // 部分浏览器对 currentTime=自身 会 no-op；极小回跳才能强制音视频轨重对齐
    const seekTo = t <= 0.001 ? t + 0.001 : t - 0.001;
    softSeekMedia(video, seekTo);
    softSeekMedia(audio, seekTo);
    lastVideoTime = seekTo;
    lastWallMs = now;
    sawFreeze = false;
    pendingWaitingResync = false;
    onResync?.({ driftSec, reason });
  };

  const onPlay = () => {
    syncAudioFromVideo(true);
    void audio.play().catch(() => undefined);
  };

  const onPause = () => {
    audio.pause();
  };

  const onSeeked = () => {
    if (syncingAudio) return;
    syncAudioFromVideo(true);
    if (!video.paused) {
      void audio.play().catch(() => undefined);
    }
  };

  const onRateChange = () => {
    audio.playbackRate = video.playbackRate || 1;
  };

  const onWaiting = () => {
    if (!video.paused) pendingWaitingResync = true;
  };

  const onPlaying = () => {
    if (!pendingWaitingResync) return;
    correct('waiting', audio.currentTime - video.currentTime);
  };

  const onLoadedMeta = () => {
    syncAudioFromVideo(true);
  };

  video.addEventListener('play', onPlay);
  video.addEventListener('pause', onPause);
  video.addEventListener('seeked', onSeeked);
  video.addEventListener('ratechange', onRateChange);
  video.addEventListener('waiting', onWaiting);
  video.addEventListener('playing', onPlaying);
  video.addEventListener('loadedmetadata', onLoadedMeta);

  syncAudioFromVideo(true);
  if (!video.paused) {
    onPlay();
  }

  const timer = window.setInterval(() => {
    if (destroyed || video.paused || video.ended || video.seeking || audio.seeking || syncingAudio) {
      lastVideoTime = video.currentTime || 0;
      lastWallMs = performance.now();
      return;
    }

    const now = performance.now();
    const wallDeltaSec = (now - lastWallMs) / 1000;
    const mediaDeltaSec = (video.currentTime || 0) - lastVideoTime;

    if (
      detectPlaybackFreeze({
        paused: video.paused,
        seeking: video.seeking,
        wallDeltaSec,
        mediaDeltaSec,
      })
    ) {
      sawFreeze = true;
    } else if (sawFreeze && mediaDeltaSec > 0.08) {
      correct('freeze', audio.currentTime - video.currentTime);
    }

    const driftSec = (audio.currentTime || 0) - (video.currentTime || 0);
    if (
      audio.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA &&
      shouldCorrectAvDrift(driftSec, thresholdSec)
    ) {
      correct('clock_drift', driftSec);
    }

    lastVideoTime = video.currentTime || 0;
    lastWallMs = now;
  }, checkIntervalMs);

  return {
    destroy: () => {
      if (destroyed) return;
      destroyed = true;
      window.clearInterval(timer);
      video.removeEventListener('play', onPlay);
      video.removeEventListener('pause', onPause);
      video.removeEventListener('seeked', onSeeked);
      video.removeEventListener('ratechange', onRateChange);
      video.removeEventListener('waiting', onWaiting);
      video.removeEventListener('playing', onPlaying);
      video.removeEventListener('loadedmetadata', onLoadedMeta);
      audio.pause();
      audio.removeAttribute('src');
      audio.load();
      audio.remove();
    },
  };
}
