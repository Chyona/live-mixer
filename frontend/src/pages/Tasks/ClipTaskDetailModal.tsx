import { useEffect, useState } from 'react';
import { Button, Descriptions, Drawer, Progress, Typography } from 'antd';
import { LuDownload, LuPackage, LuX } from 'react-icons/lu';

import { AppError } from '~/services/http';
import {
  downloadTaskClipsTar,
  downloadTaskVideo,
  type ClipTaskItem,
} from '~/services/task';
import { formatToDateTime } from '~/utils/date';
import { showAppError, toast } from '~/utils/toast';
import {
  getClipTaskAspectRatio,
  getClipTaskDisplayName,
  getClipTaskLiveName,
  getClipTaskStatusLabel,
  getGenerationTaskTypeLabel,
} from './utils';

interface ClipTaskDetailModalProps {
  open: boolean;
  task: ClipTaskItem | null;
  onClose: () => void;
}

type DetailDownloadAction = 'video' | 'clips-tar';

function EmptyValue() {
  return <span className="tasks-error-empty">-</span>;
}

function CopyableUrl({ url }: { url: string }) {
  const text = url.trim();
  if (!text) return <EmptyValue />;

  return (
    <Typography.Paragraph className="tasks-detail-draft" copyable={{ text }}>
      {text}
    </Typography.Paragraph>
  );
}

const ClipTaskDetailModal = ({ open, task, onClose }: ClipTaskDetailModalProps) => {
  const [promptOpen, setPromptOpen] = useState(false);
  const [downloadAction, setDownloadAction] = useState<DetailDownloadAction | null>(null);
  const draftUrl = task?.draft_url?.trim() || '';
  const liveUrl = task?.live_url?.trim() || '';
  const videoUrl = task?.video_url?.trim() || '';
  const clipsTarUrl = task?.clips_tar_url?.trim() || '';
  const errorMessage = task?.error_message?.trim() || '';
  const sysPrompt = task?.sys_prompt?.trim() || '';
  const projectName = task ? getClipTaskDisplayName(task) : '';
  const liveName = task ? getClipTaskLiveName(task) : '';
  const aspectRatio = task ? getClipTaskAspectRatio(task) : '-';
  const percent = task ? Math.max(0, Math.min(100, Math.round(task.progress))) : 0;
  const isCompleted = task?.status === 'completed';
  const isFailed = task?.status === 'failed';
  const progressStatus = isCompleted ? 'success' : isFailed ? 'exception' : 'normal';

  useEffect(() => {
    if (!open) {
      setPromptOpen(false);
      setDownloadAction(null);
    }
  }, [open]);

  const handleClose = () => {
    setPromptOpen(false);
    onClose();
  };

  const handleDownload = async (action: DetailDownloadAction) => {
    if (!task) return;
    const label = action === 'video' ? '视频' : '视频片段压缩包';
    setDownloadAction(action);
    toast.notify.info(`正在下载 ${label}，请稍候…`);

    try {
      if (action === 'video') {
        await downloadTaskVideo(task);
      } else {
        await downloadTaskClipsTar(task);
      }
    } catch (error) {
      if (error instanceof AppError) {
        showAppError(error);
      } else {
        toast.notify.error(error instanceof Error ? error.message : `${label}下载失败`);
      }
    } finally {
      setDownloadAction((current) => (current === action ? null : current));
    }
  };

  return (
    <>
      <Drawer
        className="tasks-detail-drawer"
        title={null}
        placement="right"
        width="min(640px, 100vw)"
        open={open}
        onClose={handleClose}
        closable={false}
        destroyOnClose
      >
        {task ? (
          <div className="tasks-detail-drawer__layout">
            <header className="tasks-detail-drawer__header">
              <div className="tasks-detail-drawer__header-main">
                <h3 className="tasks-detail-drawer__title">任务详情</h3>
                <p className="tasks-detail-drawer__meta">
                  <span className={`tasks-type-label tasks-type-label_${task.type || 'draft'}`}>
                    {getGenerationTaskTypeLabel(task.type)}
                  </span>
                  <span className="tasks-detail-drawer__meta-sep">·</span>
                  <span className={`tasks-status tasks-status_${task.status}`}>
                    <span className="tasks-status-dot" aria-hidden />
                    {getClipTaskStatusLabel(task.status)}
                  </span>
                </p>
              </div>
              <button
                type="button"
                className="tasks-detail-drawer__close"
                aria-label="关闭"
                onClick={handleClose}
              >
                <LuX size={18} />
              </button>
            </header>

            <div className="tasks-detail-drawer__body">
              <Descriptions column={1} size="small" className="tasks-detail-descriptions">
                <Descriptions.Item label="任务ID">
                  {task.id.trim() ? task.id : <EmptyValue />}
                </Descriptions.Item>
                <Descriptions.Item label="项目名称">
                  {projectName || <EmptyValue />}
                </Descriptions.Item>
                <Descriptions.Item label="源视频名称">
                  {liveName || <EmptyValue />}
                </Descriptions.Item>
                <Descriptions.Item label="系统提示词">
                  {sysPrompt ? (
                    <div className="tasks-detail-prompt-block">
                      <Typography.Paragraph
                        className="tasks-detail-prompt"
                        ellipsis={{ rows: 6 }}
                      >
                        {sysPrompt}
                      </Typography.Paragraph>
                      <button
                        type="button"
                        className="tasks-detail-prompt-more"
                        onClick={() => setPromptOpen(true)}
                      >
                        查看全文
                      </button>
                    </div>
                  ) : (
                    <EmptyValue />
                  )}
                </Descriptions.Item>
                <Descriptions.Item label="视频比例">
                  {aspectRatio === '-' ? <EmptyValue /> : aspectRatio}
                </Descriptions.Item>
                <Descriptions.Item label="任务类型">
                  <span className={`tasks-type-label tasks-type-label_${task.type || 'draft'}`}>
                    {getGenerationTaskTypeLabel(task.type)}
                  </span>
                </Descriptions.Item>
                <Descriptions.Item label="任务创建者">
                  {task.created_by?.trim() || <EmptyValue />}
                </Descriptions.Item>
                <Descriptions.Item label="任务创建时间">
                  {formatToDateTime(task.created_at)}
                </Descriptions.Item>
                <Descriptions.Item label="任务更新时间">
                  {formatToDateTime(task.updated_at)}
                </Descriptions.Item>
                <Descriptions.Item label="任务开始时间">
                  {formatToDateTime(task.started_at)}
                </Descriptions.Item>
                <Descriptions.Item label="任务完成时间">
                  {formatToDateTime(task.completed_at)}
                </Descriptions.Item>
                <Descriptions.Item label="任务状态">
                  <span className={`tasks-status tasks-status_${task.status}`}>
                    <span className="tasks-status-dot" aria-hidden />
                    {getClipTaskStatusLabel(task.status)}
                  </span>
                </Descriptions.Item>
                <Descriptions.Item label="任务进度">
                  <Progress
                    className="tasks-progress-bar tasks-detail-progress"
                    percent={isCompleted ? 100 : percent}
                    size="small"
                    status={progressStatus}
                    {...(isCompleted ? {} : { format: (value?: number) => `${value ?? 0}%` })}
                  />
                </Descriptions.Item>
                <Descriptions.Item label="错误信息">
                  {errorMessage ? (
                    <p className="tasks-detail-error">{errorMessage}</p>
                  ) : (
                    <EmptyValue />
                  )}
                </Descriptions.Item>
                <Descriptions.Item label="直播素材URL">
                  <CopyableUrl url={liveUrl} />
                </Descriptions.Item>
                <Descriptions.Item label="草稿地址">
                  <CopyableUrl url={draftUrl} />
                </Descriptions.Item>
                <Descriptions.Item label="视频下载">
                  <Button
                    type="link"
                    size="small"
                    className="tasks-detail-download-btn"
                    icon={<LuDownload size={14} />}
                    disabled={!videoUrl || (Boolean(downloadAction) && downloadAction !== 'video')}
                    loading={downloadAction === 'video'}
                    onClick={() => void handleDownload('video')}
                  >
                    下载视频
                  </Button>
                </Descriptions.Item>
                <Descriptions.Item label="片段下载">
                  <Button
                    type="link"
                    size="small"
                    className="tasks-detail-download-btn"
                    icon={<LuPackage size={14} />}
                    disabled={
                      !clipsTarUrl || (Boolean(downloadAction) && downloadAction !== 'clips-tar')
                    }
                    loading={downloadAction === 'clips-tar'}
                    onClick={() => void handleDownload('clips-tar')}
                  >
                    下载片段
                  </Button>
                </Descriptions.Item>
              </Descriptions>
            </div>
          </div>
        ) : null}
      </Drawer>

      <Drawer
        className="tasks-prompt-drawer"
        title={null}
        placement="right"
        width="min(560px, 100vw)"
        open={promptOpen}
        onClose={() => setPromptOpen(false)}
        closable={false}
        destroyOnClose
        zIndex={1100}
      >
        <div className="tasks-detail-drawer__layout">
          <header className="tasks-detail-drawer__header">
            <div className="tasks-detail-drawer__header-main">
              <h3 className="tasks-detail-drawer__title">系统提示词</h3>
            </div>
            <button
              type="button"
              className="tasks-detail-drawer__close"
              aria-label="关闭"
              onClick={() => setPromptOpen(false)}
            >
              <LuX size={18} />
            </button>
          </header>
          <div className="tasks-detail-drawer__body tasks-prompt-drawer__body">
            <pre className="tasks-prompt-drawer__content">{sysPrompt}</pre>
          </div>
        </div>
      </Drawer>
    </>
  );
};

export default ClipTaskDetailModal;
