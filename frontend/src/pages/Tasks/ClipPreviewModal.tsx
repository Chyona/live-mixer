import { Modal } from 'antd';
import StreamVideoPlayer from '~/components/StreamVideoPlayer';

interface ClipPreviewModalProps {
  open: boolean;
  url: string | null;
  title?: string;
  onClose: () => void;
}

const ClipPreviewModal = ({ open, url, title = '成片预览', onClose }: ClipPreviewModalProps) => {
  return (
    <Modal
      open={open}
      title={title}
      width={900}
      footer={null}
      destroyOnClose
      onCancel={onClose}
      className="tasks-preview-modal noanimation-modal"
    >
      {url ? (
        <StreamVideoPlayer url={url} className="tasks-preview-video" controls />
      ) : (
        <div className="tasks-preview-empty">暂无可预览的成片</div>
      )}
    </Modal>
  );
};

export default ClipPreviewModal;
