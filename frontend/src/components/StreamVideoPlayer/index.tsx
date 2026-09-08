import {
  forwardRef,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
  type VideoHTMLAttributes,
} from 'react';
import Hls from 'hls.js';
import { stripRedundantHlsDiscontinuities } from '~/utils/hlsPlaylist';
import {
  detectVideoSourceType,
  getUnsupportedVideoMessage,
  getVideoErrorMessage,
  mediaResourceKey,
  resolveVideoCrossOrigin,
  resolveVideoPlayUrl,
  type VideoSourceType,
} from '~/utils/videoUrl';

import './index.css';

export interface StreamVideoPlayerHandle {
  video: HTMLVideoElement | null;
  sourceType: VideoSourceType;
}

export interface StreamVideoPlayerProps
  extends Omit<
    VideoHTMLAttributes<HTMLVideoElement>,
    'src' | 'onError' | 'onLoadedMetadata' | 'onDurationChange'
  > {
  url: string;
  /** 加载后显示视频首帧，默认开启 */
  showFirstFrame?: boolean;
  /** 首帧 seek 时间（秒），默认视频开头附近 */
  firstFrameTime?: number;
  errorClassName?: string;
  onReady?: () => void;
  onFirstFramePrepared?: () => void;
  onDurationChange?: (duration: number) => void;
  onPlaybackError?: (message: string) => void;
  onVideoLoadedMetadata?: VideoHTMLAttributes<HTMLVideoElement>['onLoadedMetadata'];
  onVideoDurationChange?: VideoHTMLAttributes<HTMLVideoElement>['onDurationChange'];
  /** HLS 起播位置（秒）；跟播 EVENT 列表传 0，未传则用 hls.js 默认（直播边沿 / VOD 开头） */
  hlsStartPosition?: number;
}

function readDuration(video: HTMLVideoElement | null): number {
  if (!video) return 0;
  const duration = video.duration;
  return Number.isFinite(duration) && duration > 0 ? duration : 0;
}

function readHlsPlaylistDuration(hls: Hls | null): number {
  if (!hls) return 0;
  const details = hls.latestLevelDetails;
  const total = Number(details?.totalduration);
  return Number.isFinite(total) && total > 0 ? total : 0;
}

function getHlsErrorMessage(type: string, details?: string): string {
  if (details && /parse|manifest_parsing/i.test(details)) {
    return 'HLS 播放列表解析失败，请刷新后重试';
  }
  switch (type) {
    case Hls.ErrorTypes.NETWORK_ERROR:
      return 'HLS 网络加载失败，请检查 m3u8 地址是否有效';
    case Hls.ErrorTypes.MEDIA_ERROR:
      return 'HLS 媒体解析失败，请检查视频流是否可访问';
    default:
      return 'HLS 播放失败，请检查 m3u8 地址';
  }
}

const DefaultHlsLoader = Hls.DefaultConfig.loader;

/** 拦截 m3u8 响应，去掉同一录像代数内多余的 EXT-X-DISCONTINUITY。 */
class LivePlaylistLoader extends DefaultHlsLoader {
  load(context: { url?: string; type?: string }, config: unknown, callbacks: { onSuccess?: (...args: never[]) => void }) {
    const url = String(context?.url ?? '');
    const isPlaylist =
      /\.m3u8(\?|$)/i.test(url) || context?.type === 'manifest' || context?.type === 'level';
    if (isPlaylist && typeof callbacks?.onSuccess === 'function') {
      const originalSuccess = callbacks.onSuccess;
      callbacks.onSuccess = ((response: { data?: unknown }, ...rest: never[]) => {
        if (typeof response?.data === 'string') {
          response.data = stripRedundantHlsDiscontinuities(response.data);
        }
        originalSuccess(response as never, ...rest);
      }) as typeof callbacks.onSuccess;
    }
    super.load(context as never, config as never, callbacks as never);
  }
}

function waitForCanPlay(video: HTMLVideoElement, timeoutMs = 10000): Promise<void> {
  if (video.readyState >= HTMLMediaElement.HAVE_FUTURE_DATA) {
    return Promise.resolve();
  }

  return new Promise((resolve, reject) => {
    const timeout = window.setTimeout(() => {
      cleanup();
      reject(new Error('等待视频可播放超时'));
    }, timeoutMs);

    const onReady = () => {
      cleanup();
      resolve();
    };

    const cleanup = () => {
      window.clearTimeout(timeout);
      video.removeEventListener('canplay', onReady);
      video.removeEventListener('loadeddata', onReady);
    };

    video.addEventListener('canplay', onReady);
    video.addEventListener('loadeddata', onReady);
  });
}

function resolveFirstFrameTime(video: HTMLVideoElement, firstFrameTime?: number): number {
  if (firstFrameTime != null && Number.isFinite(firstFrameTime) && firstFrameTime >= 0) {
    const duration = video.duration;
    if (Number.isFinite(duration) && duration > 0) {
      return Math.min(Math.max(firstFrameTime, 0), Math.max(duration - 0.01, 0));
    }
    return firstFrameTime;
  }

  if (Number.isFinite(video.duration) && video.duration > 0) {
    return Math.min(0.1, Math.max(video.duration - 0.01, 0));
  }

  return 0.001;
}

function seekToFirstFrame(video: HTMLVideoElement, firstFrameTime?: number): Promise<boolean> {
  return new Promise((resolve) => {
    if (video.readyState < HTMLMediaElement.HAVE_METADATA) {
      resolve(false);
      return;
    }

    const seekTime = resolveFirstFrameTime(video, firstFrameTime);

    const timeout = window.setTimeout(() => {
      cleanup();
      resolve(video.readyState >= HTMLMediaElement.HAVE_CURRENT_DATA);
    }, 2000);

    const onSeeked = () => {
      cleanup();
      resolve(true);
    };

    const cleanup = () => {
      window.clearTimeout(timeout);
      video.removeEventListener('seeked', onSeeked);
    };

    video.addEventListener('seeked', onSeeked);
    video.currentTime = seekTime;
  });
}

async function renderFirstFrame(
  video: HTMLVideoElement,
  firstFrameTime?: number,
  skipPlaybackSeek = false
): Promise<boolean> {
  if (!video.paused) return true;

  try {
    await waitForCanPlay(video);
  } catch {
    if (skipPlaybackSeek) {
      return video.readyState >= HTMLMediaElement.HAVE_METADATA;
    }
    return seekToFirstFrame(video, firstFrameTime);
  }

  if (skipPlaybackSeek) {
    return true;
  }

  const previousMuted = video.muted;
  video.muted = true;

  try {
    await video.play();
    video.pause();
    video.currentTime = resolveFirstFrameTime(video, firstFrameTime);
    return true;
  } catch {
    return seekToFirstFrame(video, firstFrameTime);
  } finally {
    video.muted = previousMuted;
  }
}

function attachFirstFrameHandler(
  video: HTMLVideoElement,
  enabled: boolean,
  preparedRef: { current: boolean },
  preparingRef: { current: boolean },
  firstFrameTime?: number,
  onFirstFramePrepared?: () => void,
  skipPlaybackSeek = false
) {
  if (!enabled) {
    return () => undefined;
  }

  const attempt = () => {
    if (preparedRef.current || preparingRef.current || !video.paused) return;

    preparingRef.current = true;
    void renderFirstFrame(video, firstFrameTime, skipPlaybackSeek).then((ok) => {
      preparingRef.current = false;
      if (ok) {
        preparedRef.current = true;
        onFirstFramePrepared?.();
      }
    });
  };

  video.addEventListener('loadeddata', attempt);
  video.addEventListener('canplay', attempt);

  return () => {
    video.removeEventListener('loadeddata', attempt);
    video.removeEventListener('canplay', attempt);
  };
}

const StreamVideoPlayer = forwardRef<StreamVideoPlayerHandle, StreamVideoPlayerProps>(
  (
    {
      url,
      className,
      showFirstFrame = true,
      firstFrameTime,
      errorClassName,
      onReady,
      onFirstFramePrepared,
      onDurationChange,
      onPlaybackError,
      onVideoLoadedMetadata,
      onVideoDurationChange,
      hlsStartPosition,
      ...videoProps
    },
    ref
  ) => {
    const videoRef = useRef<HTMLVideoElement>(null);
    const hlsRef = useRef<Hls | null>(null);
    const onReadyRef = useRef(onReady);
    const onFirstFramePreparedRef = useRef(onFirstFramePrepared);
    const onDurationChangeRef = useRef(onDurationChange);
    const onPlaybackErrorRef = useRef(onPlaybackError);
    const firstFrameTimeRef = useRef(firstFrameTime);
    const lastEmittedDurationRef = useRef(0);
    const firstFramePreparedRef = useRef(false);
    const firstFramePreparingRef = useRef(false);
    const [errorMessage, setErrorMessage] = useState<string | null>(null);

    useEffect(() => {
      onReadyRef.current = onReady;
      onFirstFramePreparedRef.current = onFirstFramePrepared;
      onDurationChangeRef.current = onDurationChange;
      onPlaybackErrorRef.current = onPlaybackError;
      firstFrameTimeRef.current = firstFrameTime;
    });

    const sourceUrl = url.trim();
    const playUrl = useMemo(() => resolveVideoPlayUrl(sourceUrl), [sourceUrl]);
    const resourceKey = useMemo(() => mediaResourceKey(playUrl), [playUrl]);
    const stablePlayUrlRef = useRef(playUrl);
    if (mediaResourceKey(stablePlayUrlRef.current) !== resourceKey) {
      stablePlayUrlRef.current = playUrl;
    }
    const crossOrigin = useMemo(() => resolveVideoCrossOrigin(stablePlayUrlRef.current), [resourceKey]);
    const sourceType = useMemo(() => detectVideoSourceType(sourceUrl), [sourceUrl]);

    useImperativeHandle(ref, () => ({
      video: videoRef.current,
      sourceType,
    }));

    const emitDuration = () => {
      let duration = readDuration(videoRef.current);
      if (duration <= 0) {
        duration = readHlsPlaylistDuration(hlsRef.current);
      }
      if (duration <= 0) {
        onDurationChangeRef.current?.(0);
        return;
      }
      if (Math.abs(duration - lastEmittedDurationRef.current) < 0.05) return;
      lastEmittedDurationRef.current = duration;
      onDurationChangeRef.current?.(duration);
    };

    const emitError = (message: string) => {
      setErrorMessage(message);
      onPlaybackErrorRef.current?.(message);
    };

    useEffect(() => {
      firstFramePreparedRef.current = false;
      firstFramePreparingRef.current = false;
      lastEmittedDurationRef.current = 0;
    }, [resourceKey]);

    useEffect(() => {
      const video = videoRef.current;
      const attachedUrl = stablePlayUrlRef.current;
      if (!video || !attachedUrl) return;

      setErrorMessage(null);
      firstFramePreparedRef.current = false;
      firstFramePreparingRef.current = false;

      const unsupportedMessage = getUnsupportedVideoMessage(sourceUrl);
      if (unsupportedMessage) {
        emitError(unsupportedMessage);
        return;
      }

      if (crossOrigin) {
        video.crossOrigin = crossOrigin;
      } else {
        video.removeAttribute('crossorigin');
      }

      const destroyHls = () => {
        if (hlsRef.current) {
          hlsRef.current.destroy();
          hlsRef.current = null;
        }
      };

      const detachFirstFrameHandler = attachFirstFrameHandler(
        video,
        showFirstFrame,
        firstFramePreparedRef,
        firstFramePreparingRef,
        firstFrameTimeRef.current,
        () => onFirstFramePreparedRef.current?.(),
        sourceType === 'hls'
      );

      const handleReady = () => {
        emitDuration();
        onReadyRef.current?.();
      };

      if (sourceType === 'hls') {
        destroyHls();
        video.removeAttribute('src');

        const nativeHls =
          Boolean(
            video.canPlayType('application/vnd.apple.mpegurl') ||
              video.canPlayType('application/x-mpegURL')
          );

        if (Hls.isSupported()) {
          const startPosition =
            hlsStartPosition != null && Number.isFinite(hlsStartPosition) ? hlsStartPosition : -1;
          const hls = new Hls({
            loader: LivePlaylistLoader,
            xhrSetup: (xhr) => {
              xhr.withCredentials = false;
            },
            startPosition,
            liveDurationInfinity: false,
            maxBufferHole: 2,
            manifestLoadingMaxRetry: 6,
            levelLoadingMaxRetry: 6,
            fragLoadingMaxRetry: 6,
          });
          hlsRef.current = hls;
          hls.loadSource(attachedUrl);
          hls.attachMedia(video);
          let recoveries = 0;
          const onManifestParsed = () => {
            handleReady();
          };
          const onLevelLoaded = () => {
            emitDuration();
          };
          const onHlsError = (
            _: string,
            data: { fatal?: boolean; type: string; details?: string }
          ) => {
            if (!data.fatal) return;
            if (recoveries < 4) {
              recoveries += 1;
              if (data.type === Hls.ErrorTypes.MEDIA_ERROR) {
                hls.recoverMediaError();
                return;
              }
              if (data.type === Hls.ErrorTypes.NETWORK_ERROR) {
                hls.startLoad();
                return;
              }
            }
            emitError(getHlsErrorMessage(data.type, data.details));
          };
          hls.on(Hls.Events.MANIFEST_PARSED, onManifestParsed);
          hls.on(Hls.Events.LEVEL_LOADED, onLevelLoaded);
          hls.on(Hls.Events.ERROR, onHlsError);

          return () => {
            hls.off(Hls.Events.MANIFEST_PARSED, onManifestParsed);
            hls.off(Hls.Events.LEVEL_LOADED, onLevelLoaded);
            hls.off(Hls.Events.ERROR, onHlsError);
            detachFirstFrameHandler();
            destroyHls();
            video.pause();
            video.removeAttribute('src');
            video.load();
          };
        }

        if (nativeHls) {
          video.src = attachedUrl;
          video.load();
        } else {
          emitError('当前浏览器不支持 HLS (m3u8) 播放');
        }
      } else {
        destroyHls();
        video.src = attachedUrl;
        video.load();
      }

      return () => {
        detachFirstFrameHandler();
        destroyHls();
        video.pause();
        video.removeAttribute('src');
        video.load();
      };
    }, [crossOrigin, resourceKey, showFirstFrame, firstFrameTime, sourceType, sourceUrl, hlsStartPosition]);

    const handleLoadedMetadata = (event: React.SyntheticEvent<HTMLVideoElement>) => {
      emitDuration();
      onReadyRef.current?.();
      onVideoLoadedMetadata?.(event);
    };

    const handleDurationChange = (event: React.SyntheticEvent<HTMLVideoElement>) => {
      emitDuration();
      onVideoDurationChange?.(event);
    };

    const handleVideoError = () => {
      if (hlsRef.current) return;
      emitError(getVideoErrorMessage(videoRef.current?.error));
    };

    return (
      <div className="stream-video-player">
        <video
          ref={videoRef}
          className={className}
          controls
          preload={showFirstFrame ? 'auto' : 'metadata'}
          playsInline
          onLoadedMetadata={handleLoadedMetadata}
          onDurationChange={handleDurationChange}
          onError={handleVideoError}
          {...videoProps}
          crossOrigin={crossOrigin || undefined}
        />
        {errorMessage && (
          <p className={errorClassName ?? 'stream-video-player__error'}>{errorMessage}</p>
        )}
      </div>
    );
  }
);

StreamVideoPlayer.displayName = 'StreamVideoPlayer';

export default StreamVideoPlayer;
