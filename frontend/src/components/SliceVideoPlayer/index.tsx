import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useRef,
  useState,
} from 'react';
import StreamVideoPlayer, {
  type StreamVideoPlayerHandle,
  type StreamVideoPlayerProps,
} from '~/components/StreamVideoPlayer';
import { attachAvDriftResync } from '~/utils/avDriftResync';
import type { VideoPlayerTranscriptParagraph } from '~/utils/videoPlayerTools';
import VideoPlayerToolbar from './VideoPlayerToolbar';

import './index.css';

export interface SliceVideoPlayerProps extends StreamVideoPlayerProps {
  paragraphs?: VideoPlayerTranscriptParagraph[];
  currentTime?: number;
  onSeek?: (time: number) => void;
  screenshotBaseName?: string;
  enableToolbar?: boolean;
}

const SliceVideoPlayer = forwardRef<StreamVideoPlayerHandle, SliceVideoPlayerProps>(
  (
    {
      paragraphs = [],
      currentTime: controlledCurrentTime,
      onSeek: externalOnSeek,
      screenshotBaseName = 'video-screenshot',
      enableToolbar = true,
      className,
      onReady,
      onDurationChange,
      ...videoProps
    },
    ref
  ) => {
    const playerRef = useRef<StreamVideoPlayerHandle>(null);
    const [videoEl, setVideoEl] = useState<HTMLVideoElement | null>(null);
    const [internalCurrentTime, setInternalCurrentTime] = useState(0);

    useImperativeHandle(ref, () => ({
      get video() {
        return playerRef.current?.video ?? null;
      },
      get sourceType() {
        return playerRef.current?.sourceType ?? 'unsupported';
      },
    }));

    const currentTime = controlledCurrentTime ?? internalCurrentTime;

    const syncVideoEl = useCallback(() => {
      setVideoEl(playerRef.current?.video ?? null);
    }, []);

    const handleSeek = useCallback(
      (time: number) => {
        if (externalOnSeek) {
          externalOnSeek(time);
          return;
        }

        const video = playerRef.current?.video;
        if (!video) return;

        video.currentTime = time;
        setInternalCurrentTime(time);
        if (video.paused) {
          void video.play().catch(() => undefined);
        }
      },
      [externalOnSeek]
    );

    useEffect(() => {
      if (controlledCurrentTime != null) return;

      const video = playerRef.current?.video;
      if (!video) return;

      const syncCurrentTime = () => {
        setInternalCurrentTime(video.currentTime || 0);
      };

      syncCurrentTime();
      video.addEventListener('timeupdate', syncCurrentTime);
      video.addEventListener('seeked', syncCurrentTime);

      return () => {
        video.removeEventListener('timeupdate', syncCurrentTime);
        video.removeEventListener('seeked', syncCurrentTime);
      };
    }, [controlledCurrentTime, videoEl, videoProps.url]);

    // 原生 mp4（跟播 master）预览兜底：音视频钟漂移 / 卡顿后 soft-seek 纠偏
    useEffect(() => {
      const video = playerRef.current?.video;
      const sourceType = playerRef.current?.sourceType;
      if (!video || sourceType !== 'native') return;

      const handle = attachAvDriftResync(video);
      return () => handle.destroy();
    }, [videoEl, videoProps.url]);

    return (
      <div className="slice-video-player-frame">
        <StreamVideoPlayer
          ref={playerRef}
          className={className}
          onReady={() => {
            syncVideoEl();
            onReady?.();
          }}
          onDurationChange={(duration) => {
            syncVideoEl();
            onDurationChange?.(duration);
          }}
          {...videoProps}
        />
        {enableToolbar ? (
          <VideoPlayerToolbar
            video={videoEl}
            paragraphs={paragraphs}
            currentTime={currentTime}
            screenshotBaseName={screenshotBaseName}
            onSeek={handleSeek}
          />
        ) : null}
      </div>
    );
  }
);

SliceVideoPlayer.displayName = 'SliceVideoPlayer';

export default SliceVideoPlayer;
