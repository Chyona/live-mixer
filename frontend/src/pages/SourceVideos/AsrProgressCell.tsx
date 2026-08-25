import { Button, Progress, Tooltip } from 'antd';
import { LuCircleAlert, LuHourglass, LuRotateCw } from 'react-icons/lu';

import type { AsrStatus } from '~/services/sourceVideo';

import { ASR_STATUS_LABEL } from './asrUtils';

function getProgressStatus(status: AsrStatus): 'success' | 'exception' | 'active' | 'normal' {
  switch (status) {
    case 'completed':
      return 'success';
    case 'failed':
      return 'exception';
    case 'processing':
      return 'active';
    default:
      return 'normal';
  }
}

interface AsrProgressCellProps {
  status: AsrStatus;
  progress: number;
  errorMessage?: string;
  retrying?: boolean;
  onRetry?: () => void;
}

const AsrProgressCell = ({ status, progress, errorMessage, retrying, onRetry }: AsrProgressCellProps) => {
  const label = ASR_STATUS_LABEL[status];

  if (status === 'pending') {
    return (
      <div className="source-videos-asr source-videos-asr_pending">
        <LuHourglass size={14} className="source-videos-asr-pending-icon" aria-hidden />
        <span className="source-videos-asr-pending-text">{label}</span>
      </div>
    );
  }

  if (status === 'failed') {
    const tooltipMessage = errorMessage?.trim();
    const failedLabel = (
      <span className="source-videos-asr-failed-text">
        <LuCircleAlert size={14} aria-hidden />
        解析失败
      </span>
    );

    return (
      <div className="source-videos-asr source-videos-asr_failed">
        {tooltipMessage ? <Tooltip title={tooltipMessage}>{failedLabel}</Tooltip> : failedLabel}
        {onRetry ? (
          <Button
            type="link"
            size="small"
            className="source-videos-asr-retry-btn"
            icon={<LuRotateCw size={14} />}
            loading={retrying}
            onClick={onRetry}
          >
            重新解析
          </Button>
        ) : null}
      </div>
    );
  }

  return (
    <div className="source-videos-asr">
      <div className="source-videos-asr-progress" title={label}>
        <Progress
          percent={progress}
          size="small"
          status={getProgressStatus(status)}
          showInfo
        />
      </div>
    </div>
  );
};

export default AsrProgressCell;
