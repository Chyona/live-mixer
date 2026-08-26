import { Dropdown, Tooltip } from 'antd';
import type { MenuProps } from 'antd';
import { useEffect, useState, type ReactNode } from 'react';
import { LuCamera, LuChevronLeft, LuChevronRight, LuGauge } from 'react-icons/lu';
import { toast } from '~/utils/toast';
import {
  PLAYBACK_RATE_OPTIONS,
  type PlaybackRateOption,
  type VideoPlayerTranscriptParagraph,
  canSeekAdjacentSentence,
  captureVideoScreenshot,
  getAdjacentSentenceSeekTime,
  getScreenshotErrorNotify,
  sanitizeScreenshotFilename,
} from '~/utils/videoPlayerTools';

interface VideoPlayerToolbarProps {
  video: HTMLVideoElement | null;
  paragraphs: VideoPlayerTranscriptParagraph[];
  currentTime: number;
  screenshotBaseName: string;
  onSeek: (time: number) => void;
}

function formatPlaybackRate(rate: PlaybackRateOption) {
  return rate === 1 ? '1x' : `${rate}x`;
}

function IconButton({
  label,
  disabled,
  onClick,
  children,
}: {
  label: string;
  disabled?: boolean;
  onClick?: () => void;
  children: ReactNode;
}) {
  const button = (
    <button
      type="button"
      className="slice-video-player-toolbar-btn"
      disabled={disabled}
      onClick={onClick}
      aria-label={label}
    >
      {children}
    </button>
  );

  return (
    <Tooltip title={label} mouseEnterDelay={0.2}>
      <span
        className={[
          'slice-video-player-toolbar-btn-wrap',
          disabled ? 'slice-video-player-toolbar-btn-wrap_disabled' : '',
        ]
          .filter(Boolean)
          .join(' ')}
      >
        {button}
      </span>
    </Tooltip>
  );
}

const VideoPlayerToolbar = ({
  video,
  paragraphs,
  currentTime,
  screenshotBaseName,
  onSeek,
}: VideoPlayerToolbarProps) => {
  const [playbackRate, setPlaybackRate] = useState<PlaybackRateOption>(1);

  useEffect(() => {
    if (!video) return;
    video.playbackRate = playbackRate;
  }, [playbackRate, video]);

  const canPrev = canSeekAdjacentSentence(paragraphs, currentTime, 'prev');
  const canNext = canSeekAdjacentSentence(paragraphs, currentTime, 'next');

  const speedMenuItems: MenuProps['items'] = PLAYBACK_RATE_OPTIONS.map((rate) => ({
    key: String(rate),
    label: formatPlaybackRate(rate),
  }));

  const handlePrevSentence = () => {
    const time = getAdjacentSentenceSeekTime(paragraphs, currentTime, 'prev');
    if (time == null) return;
    onSeek(time);
  };

  const handleNextSentence = () => {
    const time = getAdjacentSentenceSeekTime(paragraphs, currentTime, 'next');
    if (time == null) return;
    onSeek(time);
  };

  const handleScreenshot = async () => {
    if (!video) {
      toast.notify.warning('视频尚未就绪，请稍后再试');
      return;
    }

    const stamp = new Date().toISOString().replace(/[:.]/g, '-');
    const filename = `${sanitizeScreenshotFilename(screenshotBaseName)}_${stamp}.png`;

    try {
      await captureVideoScreenshot(video, filename);
      toast.notify.success('截图已保存');
    } catch (error) {
      const notify = getScreenshotErrorNotify(error);
      if (notify.type === 'warning') {
        toast.notify.warning(notify.title);
        return;
      }
      toast.notify.error(notify.title, notify.description);
    }
  };

  return (
    <div className="slice-video-player-toolbar" onClick={(event) => event.stopPropagation()}>
      <Dropdown
        menu={{
          items: speedMenuItems,
          selectedKeys: [String(playbackRate)],
          onClick: ({ key }) => {
            const nextRate = Number(key) as PlaybackRateOption;
            if (PLAYBACK_RATE_OPTIONS.includes(nextRate)) {
              setPlaybackRate(nextRate);
            }
          },
        }}
        trigger={['click']}
        placement="bottomRight"
      >
        <Tooltip title="播放速度" mouseEnterDelay={0.2}>
          <button type="button" className="slice-video-player-toolbar-btn" aria-label="播放速度">
            <LuGauge size={15} aria-hidden />
            <span className="slice-video-player-toolbar-rate">{formatPlaybackRate(playbackRate)}</span>
          </button>
        </Tooltip>
      </Dropdown>

      <IconButton label="前一句" disabled={!canPrev} onClick={handlePrevSentence}>
        <LuChevronLeft size={16} aria-hidden />
      </IconButton>

      <IconButton label="后一句" disabled={!canNext} onClick={handleNextSentence}>
        <LuChevronRight size={16} aria-hidden />
      </IconButton>

      <IconButton label="截屏" onClick={() => void handleScreenshot()}>
        <LuCamera size={15} aria-hidden />
      </IconButton>
    </div>
  );
};

export default VideoPlayerToolbar;
