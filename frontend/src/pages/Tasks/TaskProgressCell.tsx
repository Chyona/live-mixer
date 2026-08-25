import { Progress, Tooltip } from 'antd';
import { LuCircleAlert, LuHourglass } from 'react-icons/lu';

import type { ClipTaskItemStatus } from '~/services/task';

import { getClipTaskStatusLabel } from './utils';

interface TaskProgressCellProps {
  progress: number;
  status: ClipTaskItemStatus;
  errorMessage?: string;
}

/** 任务进度：等待/失败样式对齐源视频 ASR */
function TaskProgressCell({ progress, status, errorMessage }: TaskProgressCellProps) {
  const percent = Math.max(0, Math.min(100, Math.round(progress)));
  const isCompleted = status === 'completed';
  const isFailed = status === 'failed';
  const isPending = status === 'pending';
  const isProcessing = status === 'processing';

  if (isPending) {
    return (
      <div className="tasks-progress-cell tasks-progress-cell_pending">
        <LuHourglass size={14} className="tasks-progress-pending-icon" aria-hidden />
        <span className="tasks-progress-pending-text">{getClipTaskStatusLabel('pending')}</span>
      </div>
    );
  }

  if (isFailed) {
    const tooltipMessage = errorMessage?.trim();
    const failedLabel = (
      <span className="tasks-progress-failed-text">
        <LuCircleAlert size={14} aria-hidden />
        {getClipTaskStatusLabel('failed')}
      </span>
    );

    return (
      <div className="tasks-progress-cell tasks-progress-cell_failed">
        {tooltipMessage ? <Tooltip title={tooltipMessage}>{failedLabel}</Tooltip> : failedLabel}
      </div>
    );
  }

  return (
    <div className="tasks-progress-cell">
      <Progress
        className="tasks-progress-bar"
        percent={isCompleted ? 100 : percent}
        size="small"
        status={isCompleted ? 'success' : isProcessing ? 'active' : 'normal'}
        {...(isCompleted ? {} : { format: (value?: number) => `${value ?? 0}%` })}
      />
    </div>
  );
}

export default TaskProgressCell;
