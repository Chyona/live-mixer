import { useCallback, useMemo, useState, type ReactNode } from 'react';
import { Button, Dropdown, Space } from 'antd';
import type { MenuProps } from 'antd';
import type { ColumnsType, TablePaginationConfig } from 'antd/es/table';
import { LuDownload, LuEllipsis, LuFileText, LuPackage } from 'react-icons/lu';

import EllipsisTooltip from '~/components/EllipsisTooltip';
import { CopyIcon } from '~/components/CopyIcon';
import ListPageTable from '~/components/ListPageTable';
import type { ListTableEmptyProps } from '~/components/ListTableEmpty';
import {
  canDownloadTaskOutputs,
  downloadTaskClipsTar,
  downloadTaskVideo,
  type ClipTaskItem,
  type GenerationTaskType,
} from '~/services/task';
import { formatToDateTime } from '~/utils/date';
import { toast } from '~/utils/toast';

import ClipTaskDetailModal from './ClipTaskDetailModal';
import TaskProgressCell from './TaskProgressCell';
import {
  copyTextToClipboard,
  getClipTaskDisplayName,
  getClipTaskLiveName,
  getClipTaskStatusLabel,
  getGenerationTaskTypeLabel,
} from './utils';

interface ClipTaskListProps {
  tasks: ClipTaskItem[];
  loading?: boolean;
  pagination: TablePaginationConfig | false;
  onTableChange: (pagination: TablePaginationConfig) => void;
  onChanged: () => Promise<void>;
  empty?: ListTableEmptyProps;
}

type DownloadAction = 'video' | 'clips-tar';

function canCopyDraft(taskType: GenerationTaskType) {
  return taskType === 'draft' || taskType === 'ai_slice_draft';
}

function menuIcon(icon: ReactNode) {
  return <span className="app-menu-item-icon">{icon}</span>;
}

function renderTaskTypeLabel(taskType: GenerationTaskType) {
  const type = taskType || 'draft';

  return (
    <span className={`tasks-type-label tasks-type-label_${type}`}>
      {getGenerationTaskTypeLabel(type)}
    </span>
  );
}

function ClipTaskList({
  tasks,
  loading = false,
  pagination,
  onTableChange,
  empty,
}: ClipTaskListProps) {
  const [detailTask, setDetailTask] = useState<ClipTaskItem | null>(null);

  const handleCopyDraft = useCallback(async (url: string) => {
    const draftUrl = url.trim();
    if (!draftUrl) {
      toast.notify.warning('暂无草稿地址');
      return;
    }
    const copied = await copyTextToClipboard(draftUrl);
    if (copied) {
      toast.notify.success('草稿地址已复制', '请打开「剪映小助手」粘贴导入');
      return;
    }
    toast.notify.error('复制失败，请手动复制链接');
  }, []);

  const handleDownload = useCallback((task: ClipTaskItem, action: DownloadAction) => {
    const label = action === 'video' ? '视频' : '视频片段压缩包';

    try {
      if (action === 'video') {
        downloadTaskVideo(task);
      } else {
        downloadTaskClipsTar(task);
      }
    } catch (error) {
      toast.notify.error(error instanceof Error ? error.message : `${label}下载失败`);
    }
  }, []);

  const columns = useMemo<ColumnsType<ClipTaskItem>>(
    () => [
      {
        title: '任务类型',
        dataIndex: 'type',
        key: 'type',
        width: 120,
        render: (taskType: GenerationTaskType) => renderTaskTypeLabel(taskType),
      },
      {
        title: '项目名称',
        dataIndex: 'video_project_name',
        key: 'video_project_name',
        ellipsis: true,
        render: (_, record) => (
          <EllipsisTooltip text={getClipTaskDisplayName(record)} className="tasks-cell-ellipsis" />
        ),
      },
      {
        title: '源视频名称',
        dataIndex: 'live_name',
        key: 'live_name',
        ellipsis: true,
        render: (_, record) => {
          const name = getClipTaskLiveName(record);
          if (!name) return <span className="tasks-error-empty">-</span>;
          return <EllipsisTooltip text={name} className="tasks-cell-ellipsis" />;
        },
      },
      {
        title: '状态',
        dataIndex: 'status',
        key: 'status',
        width: 100,
        render: (status: ClipTaskItem['status']) => (
          <span className={`tasks-status tasks-status_${status}`}>
            <span className="tasks-status-dot" aria-hidden />
            {getClipTaskStatusLabel(status)}
          </span>
        ),
      },
      {
        title: '进度',
        dataIndex: 'progress',
        key: 'progress',
        width: 160,
        render: (progress: number, record) => (
          <TaskProgressCell
            progress={progress}
            status={record.status}
            errorMessage={record.error_message}
          />
        ),
      },
      {
        title: '创建者',
        dataIndex: 'created_by',
        key: 'created_by',
        width: 120,
        ellipsis: true,
        render: (value: string) => {
          const name = value?.trim() || '';
          if (!name) return <span className="tasks-error-empty">-</span>;
          return <EllipsisTooltip text={name} className="tasks-cell-ellipsis" />;
        },
      },
      {
        title: '创建时间',
        dataIndex: 'created_at',
        key: 'created_at',
        width: 170,
        render: (value: string) => formatToDateTime(value),
      },
      {
        title: '操作',
        key: 'actions',
        width: 110,
        fixed: 'right',
        render: (_, record) => {
          const draftUrl = record.draft_url?.trim() || '';
          const videoUrl = record.video_url?.trim() || '';
          const clipsTarUrl = record.clips_tar_url?.trim() || '';
          // AI 选片仅「详情」；生成草稿 / 一键成片显示三点菜单
          const showMoreActions = canCopyDraft(record.type);
          const canDownload = canDownloadTaskOutputs(record);

          const menuItems: MenuProps['items'] = showMoreActions
            ? [
              {
                key: 'draft',
                icon: menuIcon(<CopyIcon />),
                label: '草稿地址',
                disabled: !draftUrl,
                onClick: () => void handleCopyDraft(draftUrl),
              },
              {
                key: 'video',
                icon: menuIcon(<LuDownload size={14} />),
                label: '视频下载',
                disabled: !videoUrl || !canDownload,
                onClick: () => handleDownload(record, 'video'),
              },
              {
                key: 'clips-tar',
                icon: menuIcon(<LuPackage size={14} />),
                label: '视频片段下载',
                disabled: !clipsTarUrl || !canDownload,
                onClick: () => handleDownload(record, 'clips-tar'),
              },
            ]
            : [];

          return (
            <Space size={4}>
              <Button
                type="link"
                size="small"
                className="list-page__action-btn"
                icon={<LuFileText size={14} />}
                onClick={() => setDetailTask(record)}
              >
                详情
              </Button>
              {showMoreActions ? (
                <Dropdown
                  menu={{ items: menuItems }}
                  trigger={['hover']}
                  placement="bottomRight"
                  overlayClassName="list-page-action-dropdown"
                >
                  <Button
                    type="link"
                    size="small"
                    className="list-page__action-btn"
                    icon={<LuEllipsis size={16} />}
                    aria-label="更多操作"
                  />
                </Dropdown>
              ) : null}
            </Space>
          );
        },
      },
    ],
    [handleCopyDraft, handleDownload]
  );

  return (
    <>
      <ListPageTable<ClipTaskItem>
        className="tasks-table"
        rowKey="id"
        loading={loading}
        columns={columns}
        dataSource={tasks}
        scrollX={1330}
        empty={empty}
        pagination={pagination}
        onChange={(nextPagination) => onTableChange(nextPagination)}
      />

      <ClipTaskDetailModal open={Boolean(detailTask)} task={detailTask} onClose={() => setDetailTask(null)} />
    </>
  );
}

export default ClipTaskList;
