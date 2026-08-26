import { Modal } from 'antd';
import SliceVideoPlayer from '~/components/SliceVideoPlayer';

interface ClipPreviewModalProps {
  open: boolean;
  url: string | null;
  title?: string;
  screenshotBaseName?: string;
  onClose: () => void;
}

const ClipPreviewModal = ({
  open,
  url,
  title = '成片预览',
  screenshotBaseName = 'clip-preview',
  onClose,
}: ClipPreviewModalProps) => {
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
        <SliceVideoPlayer
          url={url}
          className="tasks-preview-video"
          controls
          screenshotBaseName={screenshotBaseName}
        />
      ) : (
        <div className="tasks-preview-empty">暂无可预览的成片</div>
      )}
    </Modal>
  );
};

export default ClipPreviewModal;
