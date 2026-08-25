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
import {
  detectVideoSourceType,
  getUnsupportedVideoMessage,
  getVideoErrorMessage,
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
  errorClassName?: string;
  onReady?: () => void;
  onDurationChange?: (duration: number) => void;
  onPlaybackError?: (message: string) => void;
  onVideoLoadedMetadata?: VideoHTMLAttributes<HTMLVideoElement>['onLoadedMetadata'];
  onVideoDurationChange?: VideoHTMLAttributes<HTMLVideoElement>['onDurationChange'];
}

function readDuration(video: HTMLVideoElement | null): number {
  if (!video) return 0;
  const duration = video.duration;
  return Number.isFinite(duration) && duration > 0 ? duration : 0;
}

function getHlsErrorMessage(type: string): string {
  switch (type) {
    case Hls.ErrorTypes.NETWORK_ERROR:
      return 'HLS 网络加载失败，请检查 m3u8 地址是否有效';
    case Hls.ErrorTypes.MEDIA_ERROR:
      return 'HLS 媒体解析失败，请检查视频流是否可访问';
    default:
      return 'HLS 播放失败，请检查 m3u8 地址';
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

function seekToFirstFrame(video: HTMLVideoElement): Promise<boolean> {
  return new Promise((resolve) => {
    if (video.readyState < HTMLMediaElement.HAVE_METADATA) {
      resolve(false);
      return;
    }

    const seekTime =
      Number.isFinite(video.duration) && video.duration > 0
        ? Math.min(0.1, Math.max(video.duration - 0.01, 0))
        : 0.001;

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

async function renderFirstFrame(video: HTMLVideoElement): Promise<boolean> {
  if (!video.paused) return true;

  try {
    await waitForCanPlay(video);
  } catch {
    return seekToFirstFrame(video);
  }

  const previousMuted = video.muted;
  video.muted = true;

  try {
    await video.play();
    video.pause();
    const seekTime =
      Number.isFinite(video.duration) && video.duration > 0
        ? Math.min(0.1, Math.max(video.duration - 0.01, 0))
        : 0.001;
    video.currentTime = seekTime;
    return true;
  } catch {
    return seekToFirstFrame(video);
  } finally {
    video.muted = previousMuted;
  }
}

function attachFirstFrameHandler(
  video: HTMLVideoElement,
  enabled: boolean,
  preparedRef: { current: boolean },
  preparingRef: { current: boolean }
) {
  if (!enabled) {
    return () => undefined;
  }

  const attempt = () => {
    if (preparedRef.current || preparingRef.current || !video.paused) return;

    preparingRef.current = true;
    void renderFirstFrame(video).then((ok) => {
      preparingRef.current = false;
      if (ok) {
        preparedRef.current = true;
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
      errorClassName,
      onReady,
      onDurationChange,
      onPlaybackError,
      onVideoLoadedMetadata,
      onVideoDurationChange,
      ...videoProps
    },
    ref
  ) => {
    const videoRef = useRef<HTMLVideoElement>(null);
    const hlsRef = useRef<Hls | null>(null);
    const onReadyRef = useRef(onReady);
    const onDurationChangeRef = useRef(onDurationChange);
    const onPlaybackErrorRef = useRef(onPlaybackError);
    const firstFramePreparedRef = useRef(false);
    const firstFramePreparingRef = useRef(false);
    const [errorMessage, setErrorMessage] = useState<string | null>(null);

    useEffect(() => {
      onReadyRef.current = onReady;
      onDurationChangeRef.current = onDurationChange;
      onPlaybackErrorRef.current = onPlaybackError;
    });

    const sourceUrl = url.trim();
    const playUrl = useMemo(() => resolveVideoPlayUrl(sourceUrl), [sourceUrl]);
    const sourceType = useMemo(() => detectVideoSourceType(sourceUrl), [sourceUrl]);

    useImperativeHandle(ref, () => ({
      video: videoRef.current,
      sourceType,
    }));

    const emitDuration = () => {
      const duration = readDuration(videoRef.current);
      if (duration > 0) {
        onDurationChangeRef.current?.(duration);
      }
    };

    const emitError = (message: string) => {
      setErrorMessage(message);
      onPlaybackErrorRef.current?.(message);
    };

    useEffect(() => {
      firstFramePreparedRef.current = false;
      firstFramePreparingRef.current = false;
    }, [playUrl]);

    useEffect(() => {
      const video = videoRef.current;
      if (!video || !playUrl) return;

      setErrorMessage(null);
      firstFramePreparedRef.current = false;
      firstFramePreparingRef.current = false;

      const unsupportedMessage = getUnsupportedVideoMessage(sourceUrl);
      if (unsupportedMessage) {
        emitError(unsupportedMessage);
        return;
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
        firstFramePreparingRef
      );

      const handleReady = () => {
        emitDuration();
        onReadyRef.current?.();
      };

      if (sourceType === 'hls') {
        destroyHls();
        video.removeAttribute('src');

        if (video.canPlayType('application/vnd.apple.mpegurl') || video.canPlayType('application/x-mpegURL')) {
          video.src = playUrl;
          video.load();
        } else if (Hls.isSupported()) {
          const hls = new Hls();
          hlsRef.current = hls;
          hls.loadSource(playUrl);
          hls.attachMedia(video);
          hls.on(Hls.Events.MANIFEST_PARSED, handleReady);
          hls.on(Hls.Events.ERROR, (_, data) => {
            if (!data.fatal) return;
            emitError(getHlsErrorMessage(data.type));
          });
        } else {
          emitError('当前浏览器不支持 HLS (m3u8) 播放');
        }
      } else {
        destroyHls();
        video.src = playUrl;
        video.load();
      }

      return () => {
        detachFirstFrameHandler();
        destroyHls();
        video.pause();
        video.removeAttribute('src');
        video.load();
      };
    }, [playUrl, showFirstFrame, sourceType, sourceUrl]);

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
