import { Modal } from 'antd';
import { useEffect, useMemo, useRef, useState } from 'react';
import {
  LuMaximize,
  LuMinimize,
  LuPause,
  LuPlay,
  LuRotateCcw,
  LuSkipBack,
  LuSkipForward,
  // LuVolume2,
  // LuVolumeX,
  LuX,
} from 'react-icons/lu';
import StreamVideoPlayer, { type StreamVideoPlayerHandle } from '~/components/StreamVideoPlayer';
import type { SelectedCopySegment } from '../types';
import { formatSliceTime } from '../utils';

interface SegmentPreviewModalProps {
  open: boolean;
  url: string;
  segments: SelectedCopySegment[];
  onClose: () => void;
}

const END_EPS = 0.06;

function getSegmentDuration(segment: SelectedCopySegment) {
  return Math.max(0, segment.end - segment.start);
}

function getComposedTotal(segments: SelectedCopySegment[]) {
  return segments.reduce((sum, segment) => sum + getSegmentDuration(segment), 0);
}

function sourceToComposed(
  segments: SelectedCopySegment[],
  index: number,
  sourceTime: number
) {
  let composed = 0;
  for (let i = 0; i < index; i += 1) {
    const segment = segments[i];
    if (segment) composed += getSegmentDuration(segment);
  }

  const current = segments[index];
  if (!current) return composed;

  const offset = Math.min(Math.max(sourceTime, current.start), current.end) - current.start;
  return composed + Math.max(0, offset);
}

function composedToSource(segments: SelectedCopySegment[], composedTime: number) {
  let remaining = Math.max(0, composedTime);

  for (let i = 0; i < segments.length; i += 1) {
    const segment = segments[i];
    if (!segment) continue;

    const duration = getSegmentDuration(segment);
    const isLast = i === segments.length - 1;
    if (remaining <= duration || isLast) {
      return {
        index: i,
        sourceTime: segment.start + Math.min(remaining, duration),
      };
    }
    remaining -= duration;
  }

  const lastIndex = Math.max(segments.length - 1, 0);
  const last = segments[lastIndex];
  return {
    index: lastIndex,
    sourceTime: last?.end ?? 0,
  };
}

function formatComposedClock(seconds: number) {
  const safe = Math.max(0, seconds);
  const mins = Math.floor(safe / 60);
  const secs = Math.floor(safe % 60);
  return `${mins}:${String(secs).padStart(2, '0')}`;
}

function seekVideo(video: HTMLVideoElement, time: number): Promise<void> {
  const target = Math.max(0, time);

  return new Promise((resolve) => {
    if (Math.abs(video.currentTime - target) < 0.04) {
      resolve();
      return;
    }

    let settled = false;
    const finish = () => {
      if (settled) return;
      settled = true;
      video.removeEventListener('seeked', onSeeked);
      window.clearTimeout(timer);
      resolve();
    };

    const onSeeked = () => finish();
    const timer = window.setTimeout(finish, 2500);

    video.addEventListener('seeked', onSeeked);
    try {
      video.currentTime = target;
    } catch {
      finish();
    }
  });
}

const SegmentPreviewModal = ({ open, url, segments, onClose }: SegmentPreviewModalProps) => {
  const playerRef = useRef<StreamVideoPlayerHandle>(null);
  const stageRef = useRef<HTMLDivElement>(null);
  const playlistRef = useRef<HTMLDivElement>(null);
  const segmentsRef = useRef(segments);
  const indexRef = useRef(0);
  const switchingRef = useRef(false);
  const watchTimerRef = useRef(0);
  const playSegmentAtRef = useRef<(index: number, sourceTime?: number) => Promise<void>>(
    async () => undefined
  );
  const [currentIndex, setCurrentIndex] = useState(0);
  const [playerReady, setPlayerReady] = useState(false);
  const [isPlaying, setIsPlaying] = useState(false);
  const [composedCurrent, setComposedCurrent] = useState(0);
  // const [volume, setVolume] = useState(0.8);
  // const [muted, setMuted] = useState(false);
  const [isFullscreen, setIsFullscreen] = useState(false);
  // const volumeBeforeMuteRef = useRef(0.8);

  segmentsRef.current = segments;

  const composedTotal = useMemo(() => getComposedTotal(segments), [segments]);

  const chapterMarks = useMemo(() => {
    let cursor = 0;
    return segments.map((segment, index) => {
      const duration = getSegmentDuration(segment);
      const start = cursor;
      cursor += duration;
      return {
        index,
        start,
        end: cursor,
        duration,
        text: segment.text,
        sourceStart: segment.start,
        sourceEnd: segment.end,
      };
    });
  }, [segments]);

  const isEnded =
    !isPlaying && composedTotal > 0 && composedCurrent >= composedTotal - 0.05;

  const stopWatch = () => {
    window.clearInterval(watchTimerRef.current);
    watchTimerRef.current = 0;
  };

  const syncComposedTime = () => {
    const video = playerRef.current?.video;
    if (!video) return;
    setComposedCurrent(
      sourceToComposed(segmentsRef.current, indexRef.current, video.currentTime)
    );
  };

  const tickWatchRef = useRef<() => void>(() => undefined);

  const ensureWatch = () => {
    if (watchTimerRef.current) return;
    watchTimerRef.current = window.setInterval(() => {
      tickWatchRef.current();
    }, 50);
  };

  const playSegmentAt = async (index: number, sourceTime?: number) => {
    const video = playerRef.current?.video;
    const segment = segmentsRef.current[index];
    if (!video || !segment) return;

    ensureWatch();
    switchingRef.current = true;
    indexRef.current = index;
    setCurrentIndex(index);

    const clamped =
      sourceTime == null
        ? segment.start
        : Math.min(Math.max(sourceTime, segment.start), Math.max(segment.end - 0.05, segment.start));

    video.pause();
    await seekVideo(video, clamped);

    if (Math.abs(video.currentTime - clamped) > 0.35) {
      await seekVideo(video, clamped);
    }

    switchingRef.current = false;
    syncComposedTime();

    try {
      await video.play();
      setIsPlaying(true);
    } catch {
      setIsPlaying(false);
    }
  };

  playSegmentAtRef.current = playSegmentAt;

  const advanceOrStop = () => {
    const list = segmentsRef.current;
    const nextIndex = indexRef.current + 1;
    const video = playerRef.current?.video;

    if (nextIndex >= list.length) {
      video?.pause();
      setIsPlaying(false);
      setComposedCurrent(getComposedTotal(list));
      return;
    }

    void playSegmentAtRef.current(nextIndex);
  };

  tickWatchRef.current = () => {
    if (switchingRef.current) return;

    const video = playerRef.current?.video;
    const segment = segmentsRef.current[indexRef.current];
    if (!video || !segment) return;

    syncComposedTime();
    if (video.paused) return;

    const time = video.currentTime;

    if (time < segment.start - 0.2) {
      void playSegmentAtRef.current(indexRef.current);
      return;
    }

    if (time >= segment.end - END_EPS) {
      advanceOrStop();
    }
  };

  useEffect(() => {
    if (!open) {
      stopWatch();
      indexRef.current = 0;
      switchingRef.current = false;
      setCurrentIndex(0);
      setPlayerReady(false);
      setIsPlaying(false);
      setComposedCurrent(0);
      return;
    }

    if (!playerReady) return;

    ensureWatch();
    void playSegmentAtRef.current(0);

    return () => {
      stopWatch();
    };
  }, [open, playerReady]);

  useEffect(() => {
    const container = playlistRef.current;
    if (!container) return;
    const active = container.querySelector<HTMLElement>(`[data-preview-index="${currentIndex}"]`);
    if (!active) return;

    const containerRect = container.getBoundingClientRect();
    const activeRect = active.getBoundingClientRect();
    const padding = 8;
    if (activeRect.top < containerRect.top + padding) {
      container.scrollTop -= containerRect.top + padding - activeRect.top;
    } else if (activeRect.bottom > containerRect.bottom - padding) {
      container.scrollTop += activeRect.bottom - (containerRect.bottom - padding);
    }
  }, [currentIndex]);

  // useEffect(() => {
  //   const video = playerRef.current?.video;
  //   if (!video) return;
  //   video.volume = volume;
  //   video.muted = muted;
  // }, [volume, muted, playerReady]);

  useEffect(() => {
    const onFullscreenChange = () => {
      const stage = stageRef.current;
      setIsFullscreen(Boolean(stage && document.fullscreenElement === stage));
    };

    document.addEventListener('fullscreenchange', onFullscreenChange);
    return () => document.removeEventListener('fullscreenchange', onFullscreenChange);
  }, []);

  useEffect(() => {
    if (open) return;
    if (document.fullscreenElement) {
      void document.exitFullscreen().catch(() => undefined);
    }
    setIsFullscreen(false);
  }, [open]);

  const handleReady = () => {
    setPlayerReady(true);
    // const video = playerRef.current?.video;
    // if (video) {
    //   video.volume = volume;
    //   video.muted = muted;
    // }
  };

  // const handleToggleMute = () => {
  //   if (muted || volume === 0) {
  //     const restore = volumeBeforeMuteRef.current || 0.8;
  //     setVolume(restore);
  //     setMuted(false);
  //     return;
  //   }
  //
  //   volumeBeforeMuteRef.current = volume;
  //   setMuted(true);
  // };
  //
  // const handleVolumeChange = (next: number) => {
  //   const clamped = Math.min(Math.max(next, 0), 1);
  //   setVolume(clamped);
  //   if (clamped === 0) {
  //     setMuted(true);
  //     return;
  //   }
  //   volumeBeforeMuteRef.current = clamped;
  //   setMuted(false);
  // };

  const handleToggleFullscreen = async () => {
    const stage = stageRef.current;
    if (!stage) return;

    try {
      if (document.fullscreenElement === stage) {
        await document.exitFullscreen();
        return;
      }
      await stage.requestFullscreen();
    } catch {
      // 浏览器策略或设备不支持时忽略
    }
  };

  const handleTogglePlay = () => {
    const video = playerRef.current?.video;
    if (!video) return;

    if (video.paused) {
      ensureWatch();
      const segment = segmentsRef.current[indexRef.current];
      const atEnd =
        composedCurrent >= composedTotal - 0.05 ||
        (segment != null &&
          video.currentTime >= segment.end - END_EPS &&
          indexRef.current >= segmentsRef.current.length - 1);

      if (atEnd) {
        void playSegmentAt(0);
        return;
      }

      if (segment && video.currentTime >= segment.end - END_EPS) {
        advanceOrStop();
        return;
      }

      void video
        .play()
        .then(() => setIsPlaying(true))
        .catch(() => setIsPlaying(false));
      return;
    }

    video.pause();
    setIsPlaying(false);
  };

  const handleSeekComposed = (composedTime: number) => {
    const mapped = composedToSource(segmentsRef.current, composedTime);
    setComposedCurrent(composedTime);
    void playSegmentAt(mapped.index, mapped.sourceTime);
  };

  const handleTrackPointer = (event: React.PointerEvent<HTMLDivElement>) => {
    if (composedTotal <= 0) return;
    const rect = event.currentTarget.getBoundingClientRect();
    const ratio = Math.min(Math.max((event.clientX - rect.left) / rect.width, 0), 1);
    handleSeekComposed(ratio * composedTotal);
  };

  const handlePrevSegment = () => {
    if (currentIndex <= 0) {
      void playSegmentAt(0);
      return;
    }
    void playSegmentAt(currentIndex - 1);
  };

  const handleNextSegment = () => {
    if (currentIndex >= segments.length - 1) return;
    void playSegmentAt(currentIndex + 1);
  };

  const progressRatio = composedTotal > 0 ? Math.min(composedCurrent / composedTotal, 1) : 0;

  return (
    <Modal
      open={open}
      title={null}
      closable={false}
      centered
      width="min(1280px, 88vw)"
      footer={null}
      destroyOnClose
      onCancel={onClose}
      className="slice-editor-preview-modal-wrap noanimation-modal"
      styles={{
        body: { maxHeight: 'min(860px, calc(100vh - 72px))', overflow: 'visible' },
      }}
    >
      <div className="slice-editor-preview-modal">
        <button
          type="button"
          className="slice-editor-preview-close"
          onClick={onClose}
          aria-label="关闭"
        >
          <LuX size={16} />
        </button>

        <div className="slice-editor-preview-stage" ref={stageRef}>
          <div className="slice-editor-preview-video-shell">
            <StreamVideoPlayer
              ref={playerRef}
              url={url}
              className="slice-editor-preview-video"
              controls={false}
              showFirstFrame={false}
              onReady={handleReady}
            />
          </div>

          <div className="slice-editor-preview-controls">
            <div className="slice-editor-preview-controls-row">
              <button
                type="button"
                className="slice-editor-preview-icon-btn"
                onClick={handlePrevSegment}
                disabled={currentIndex <= 0 && composedCurrent <= 0.05}
                aria-label="上一段"
              >
                <LuSkipBack size={16} />
              </button>
              <button
                type="button"
                className={[
                  'slice-editor-preview-play',
                  isPlaying ? 'is-playing' : '',
                  isEnded ? 'is-ended' : '',
                ]
                  .filter(Boolean)
                  .join(' ')}
                onClick={handleTogglePlay}
                aria-label={isEnded ? '重新播放' : isPlaying ? '暂停' : '播放'}
              >
                {isEnded ? (
                  <LuRotateCcw size={18} />
                ) : isPlaying ? (
                  <LuPause size={18} />
                ) : (
                  <LuPlay size={18} />
                )}
              </button>
              <button
                type="button"
                className="slice-editor-preview-icon-btn"
                onClick={handleNextSegment}
                disabled={currentIndex >= segments.length - 1}
                aria-label="下一段"
              >
                <LuSkipForward size={16} />
              </button>

              <div className="slice-editor-preview-time">
                <strong>{formatComposedClock(composedCurrent)}</strong>
                <span>/</span>
                <span>{formatComposedClock(composedTotal)}</span>
              </div>

              <div
                className={[
                  'slice-editor-preview-badge',
                  isPlaying ? 'is-live' : '',
                  isEnded ? 'is-ended' : '',
                ]
                  .filter(Boolean)
                  .join(' ')}
              >
                {isEnded
                  ? '已播完'
                  : isPlaying
                    ? `播放中 ${currentIndex + 1}/${segments.length}`
                    : `片段 ${Math.min(currentIndex + 1, segments.length)}/${segments.length}`}
              </div>

              <div className="slice-editor-preview-media-tools">
                {/* 音量控制暂隐藏
                <div className="slice-editor-preview-volume">
                  <button
                    type="button"
                    className="slice-editor-preview-icon-btn"
                    onClick={handleToggleMute}
                    aria-label={muted || volume === 0 ? '取消静音' : '静音'}
                  >
                    {muted || volume === 0 ? <LuVolumeX size={16} /> : <LuVolume2 size={16} />}
                  </button>
                  <input
                    type="range"
                    min={0}
                    max={1}
                    step={0.01}
                    value={muted ? 0 : volume}
                    onChange={(event) => handleVolumeChange(Number(event.target.value))}
                    aria-label="音量"
                  />
                </div>
                */}
                <button
                  type="button"
                  className="slice-editor-preview-icon-btn"
                  onClick={() => void handleToggleFullscreen()}
                  aria-label={isFullscreen ? '退出全屏' : '全屏'}
                >
                  {isFullscreen ? <LuMinimize size={16} /> : <LuMaximize size={16} />}
                </button>
              </div>
            </div>

            <div
              className="slice-editor-preview-track"
              onPointerDown={handleTrackPointer}
              role="slider"
              aria-valuemin={0}
              aria-valuemax={composedTotal}
              aria-valuenow={composedCurrent}
              aria-label="组合时间轴"
            >
              <div className="slice-editor-preview-track-rail">
                <div className="slice-editor-preview-chapters" aria-hidden>
                  {chapterMarks.map((mark) => {
                    const widthPercent =
                      composedTotal > 0 ? (mark.duration / composedTotal) * 100 : 0;
                    return (
                      <span
                        key={mark.index}
                        className={[
                          'slice-editor-preview-chapter',
                          mark.index === currentIndex ? 'is-active' : '',
                          mark.index < currentIndex ? 'is-passed' : '',
                        ]
                          .filter(Boolean)
                          .join(' ')}
                        style={{ width: `${widthPercent}%` }}
                      />
                    );
                  })}
                </div>
                <div
                  className="slice-editor-preview-track-fill"
                  style={{ width: `${progressRatio * 100}%` }}
                />
                <div
                  className="slice-editor-preview-track-thumb"
                  style={{ left: `${progressRatio * 100}%` }}
                />
              </div>
            </div>
          </div>
        </div>

        <aside className="slice-editor-preview-side">
          <div className="slice-editor-preview-panel">
            <div className="slice-editor-preview-playlist-title">
              播放列表
              <span>
                {segments.length} 段 · {formatComposedClock(composedTotal)}
              </span>
            </div>
            <div className="slice-editor-preview-playlist" ref={playlistRef}>
              {chapterMarks.map((mark) => (
                <button
                  key={mark.index}
                  type="button"
                  data-preview-index={mark.index}
                  className={[
                    'slice-editor-preview-playlist-item',
                    mark.index === currentIndex ? 'is-active' : '',
                    mark.index < currentIndex ? 'is-passed' : '',
                  ]
                    .filter(Boolean)
                    .join(' ')}
                  onClick={() => void playSegmentAt(mark.index)}
                >
                  <span className="slice-editor-preview-playlist-index">
                    {mark.index === currentIndex && isPlaying ? (
                      <span className="slice-editor-preview-eq" aria-hidden>
                        <i />
                        <i />
                        <i />
                      </span>
                    ) : (
                      mark.index + 1
                    )}
                  </span>
                  <span className="slice-editor-preview-playlist-body">
                    <span className="slice-editor-preview-playlist-text">
                      {mark.text || '（无文案）'}
                    </span>
                    <span className="slice-editor-preview-playlist-meta">
                      {formatComposedClock(mark.duration)} · 源片{' '}
                      {formatSliceTime(mark.sourceStart)}-{formatSliceTime(mark.sourceEnd)}
                    </span>
                  </span>
                </button>
              ))}
            </div>
          </div>
        </aside>
      </div>
    </Modal>
  );
};

export default SegmentPreviewModal;
