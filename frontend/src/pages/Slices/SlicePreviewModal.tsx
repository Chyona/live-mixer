import { Descriptions, Modal } from 'antd';
import { useEffect, useRef } from 'react';
import { LuPlay } from 'react-icons/lu';

import type { VideoSliceItem } from '~/services/slice';
import { formatVideoDuration } from '~/utils/duration';
import './SlicePreviewModal.css';

interface SlicePreviewModalProps {
  slice: VideoSliceItem | null;
  open: boolean;
  onClose: () => void;
}

function isPlayableUrl(url: string) {
  return /^https?:\/\//i.test(url);
}

const SlicePreviewModal = ({ slice, open, onClose }: SlicePreviewModalProps) => {
  const videoRef = useRef<HTMLVideoElement>(null);

  useEffect(() => {
    if (!open || !slice || !videoRef.current) return;

    const video = videoRef.current;
    video.currentTime = 0;

    if (isPlayableUrl(slice.previewUrl)) {
      void video.play().catch(() => undefined);
    }
  }, [open, slice]);

  const handleClose = () => {
    videoRef.current?.pause();
    onClose();
  };

  if (!slice) return null;

  const canPlay = isPlayableUrl(slice.previewUrl);

  return (
    <Modal
      className="noanimation-modal"
      title={`切片预览 - ${slice.name}`}
      open={open}
      footer={null}
      width={720}
      destroyOnClose
      onCancel={handleClose}
    >
      <div className="slice-preview-modal">
        <div className="slice-preview-player">
          {canPlay ? (
            <video
              ref={videoRef}
              className="slice-preview-video"
              src={slice.previewUrl}
              controls
              playsInline
              preload="metadata"
            />
          ) : (
            <div className="slice-preview-placeholder">
              <LuPlay size={48} />
              <p>当前直播流地址暂不支持浏览器直接预览</p>
              <span>{slice.liveUrl}</span>
            </div>
          )}
        </div>

        <Descriptions column={2} size="small" className="slice-preview-meta">
          <Descriptions.Item label="源视频" span={2}>
            {slice.sourceVideoName}
          </Descriptions.Item>
          <Descriptions.Item label="开始时间">
            {formatVideoDuration(slice.startTime)}
          </Descriptions.Item>
          <Descriptions.Item label="结束时间">
            {formatVideoDuration(slice.endTime)}
          </Descriptions.Item>
          <Descriptions.Item label="切片时长">
            {formatVideoDuration(slice.duration)}
          </Descriptions.Item>
          <Descriptions.Item label="直播地址" span={2}>
            {slice.liveUrl}
          </Descriptions.Item>
        </Descriptions>
      </div>
    </Modal>
  );
};

export default SlicePreviewModal;
