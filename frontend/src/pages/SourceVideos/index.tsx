import { useCallback, useEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { useNavigate } from 'react-router-dom';
import { Button, DatePicker, Input, Popconfirm, Space } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { LuCopy, LuPlus, LuScissors, LuSparkles, LuTrash2 } from 'react-icons/lu';

import DisabledActionWrap from '~/components/DisabledActionWrap';
import EllipsisTooltip from '~/components/EllipsisTooltip';
import ListPageLayout from '~/components/ListPageLayout';
import ListPageTable from '~/components/ListPageTable';
import ListSearchToolbar from '~/components/ListSearchToolbar';
import RemarkEditor from '~/components/RemarkEditor';
import TableColumnSetting, {
  type TableColumnSettingItem,
} from '~/components/TableColumnSetting';
import { useAppSEO } from '~/hooks/useAppSEO';
import { useListFilters } from '~/hooks/useListFilters';
import { useTableColumnVisibility } from '~/hooks/useTableColumnVisibility';
import { buildManualVideoSliceLink, buildSlicesListLink, buildSourceVideoSliceLink } from '~/routes/links';
import { AppError } from '~/services/http';
import {
  deleteSourceVideo,
  fetchSourceVideoList,
  retrySourceVideoAsr,
  updateSourceVideo,
  type SourceVideo,
} from '~/services/sourceVideo';
import { formatToDateTime } from '~/utils/date';
import { formatVideoDurationMs } from '~/utils/duration';
import { parseListKeywords, toApiKeywords } from '~/utils/listKeywords';
import { DEFAULT_TABLE_PAGINATION, handleTablePaginationChange } from '~/utils/table';
import { showAppError, showScopedError, handleRequestError, toast } from '~/utils/toast';

const SOURCE_VIDEOS_LIST_ERROR_SCOPE = 'source-videos-list';
/** ASR 进行中时列表静默刷新间隔（秒） */
const ASR_POLL_INTERVAL_MS = 2 * 1000;
/** v4：默认展示创建时间（旧缓存可能不含该列） */
const SOURCE_VIDEOS_COLUMN_STORAGE_KEY = 'source-videos-table-columns-v4';
const SOURCE_VIDEOS_LOCKED_COLUMN_KEYS = ['name', 'actions'];
/** 默认隐藏：ASR 开始/完成时间；创建时间默认展示 */
const SOURCE_VIDEOS_DEFAULT_HIDDEN_COLUMN_KEYS = ['asr_started_at', 'asr_updated_at'];

const SOURCE_VIDEOS_COLUMN_SETTINGS: TableColumnSettingItem[] = [
  { key: 'name', label: '源视频名称', locked: true },
  { key: 'remark', label: '备注' },
  { key: 'live_url', label: '视频URL' },
  { key: 'duration', label: '时长' },
  { key: 'asr_progress', label: 'ASR解析进度' },
  { key: 'asr_started_at', label: 'ASR开始时间' },
  { key: 'asr_updated_at', label: 'ASR完成时间' },
  { key: 'created_by', label: '创建者' },
  { key: 'created_at', label: '创建时间' },
  { key: 'project_count', label: '关联项目' },
  { key: 'actions', label: '操作', locked: true },
];

import AddSourceVideoModal from './AddSourceVideoModal';
import AsrHitsPanel from './AsrHitsPanel';
import AsrProgressCell from './AsrProgressCell';
import { getAsrActionDisabledReason } from './asrUtils';
import './index.css';

function renderSliceAction(options: {
  to: string;
  icon: ReactNode;
  label: string;
  disabledReason: string | null;
  onNavigate: (to: string) => void;
}) {
  const button = (
    <Button
      type="link"
      size="small"
      className="list-page__action-btn"
      icon={options.icon}
      disabled={!!options.disabledReason}
      onClick={() => options.onNavigate(options.to)}
    >
      {options.label}
    </Button>
  );

  return <DisabledActionWrap disabledReason={options.disabledReason}>{button}</DisabledActionWrap>;
}

const SourceVideosPage = () => {
  const navigate = useNavigate();

  useAppSEO({
    title: '源视频管理',
    path: '/source-videos',
    robots: 'noindex, nofollow',
  });

  const {
    keyword,
    setKeyword,
    appliedKeyword,
    setAppliedKeyword,
    applySearch: applyKeywordSearch,
    clearSearch: clearKeywordSearch,
    dateRange,
    handleDateChange,
    dateFilters,
  } = useListFilters();
  const [asrKeyword, setAsrKeyword] = useState('');
  const [appliedAsrKeyword, setAppliedAsrKeyword] = useState('');
  /** 文案搜索/清除后请求较慢，强制表格 mask（清除时 applied 已空，不能只靠 hasAsrKeyword） */
  const [asrMaskPending, setAsrMaskPending] = useState(false);
  const [asrMaskTip, setAsrMaskTip] = useState('正在搜索视频文案，请稍候…');
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const hasLoadedRef = useRef(false);
  const [list, setList] = useState<SourceVideo[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [deletingId, setDeletingId] = useState<number | null>(null);
  const [retryingAsrId, setRetryingAsrId] = useState<number | null>(null);
  const [addOpen, setAddOpen] = useState(false);

  const columnKeys = useMemo(
    () => SOURCE_VIDEOS_COLUMN_SETTINGS.map((item) => item.key),
    []
  );
  const { visibleKeySet, setVisibleKeys, visibleKeys, defaultVisibleKeys } =
    useTableColumnVisibility({
      storageKey: SOURCE_VIDEOS_COLUMN_STORAGE_KEY,
      columnKeys,
      lockedKeys: SOURCE_VIDEOS_LOCKED_COLUMN_KEYS,
      defaultHiddenKeys: SOURCE_VIDEOS_DEFAULT_HIDDEN_COLUMN_KEYS,
    });

  const loadList = useCallback(async (options?: { silent?: boolean; refresh?: boolean }) => {
    const silent = options?.silent ?? false;
    const refresh = options?.refresh ?? false;

    if (refresh) {
      setRefreshing(true);
    } else if (!silent) {
      setLoading(true);
    }

    try {
      const response = await fetchSourceVideoList({
        start_date: dateFilters.date,
        end_date: dateFilters.dateEnd,
        keywords: toApiKeywords(appliedKeyword),
        asr_keywords: toApiKeywords(appliedAsrKeyword),
        page,
        page_size: pageSize,
      });

      if (response.code !== 0) {
        if (!silent && !refresh) {
          showScopedError(SOURCE_VIDEOS_LIST_ERROR_SCOPE, response.message || '加载源视频列表失败');
        }
        return;
      }

      setList(response.data.list);
      setTotal(response.data.total);
      hasLoadedRef.current = true;
    } catch (error) {
      if (!silent && !refresh) {
        handleRequestError(SOURCE_VIDEOS_LIST_ERROR_SCOPE, error, '加载源视频列表失败');
      }
    } finally {
      if (refresh) {
        setRefreshing(false);
      } else if (!silent) {
        setLoading(false);
        setAsrMaskPending(false);
      }
    }
  }, [appliedAsrKeyword, appliedKeyword, dateFilters, page, pageSize]);

  useEffect(() => {
    void loadList();
  }, [loadList]);

  const hasProcessingAsr = useMemo(
    () => list.some((item) => item.asr_status === 'pending' || item.asr_status === 'processing'),
    [list]
  );

  useEffect(() => {
    if (!hasProcessingAsr) return;

    const timer = window.setInterval(() => {
      void loadList({ silent: true });
    }, ASR_POLL_INTERVAL_MS);

    return () => window.clearInterval(timer);
  }, [hasProcessingAsr, loadList]);

  const applySearch = () => {
    const nextAsr = asrKeyword.trim();
    if (nextAsr || appliedAsrKeyword) {
      setAsrMaskTip(
        nextAsr ? '正在搜索视频文案，请稍候…' : '正在清除文案筛选，请稍候…'
      );
      setAsrMaskPending(true);
    }
    applyKeywordSearch();
    setAppliedAsrKeyword(nextAsr);
    setPage(1);
  };

  const clearSearch = () => {
    if (appliedAsrKeyword) {
      setAsrMaskTip('正在清除文案筛选，请稍候…');
      setAsrMaskPending(true);
    }
    clearKeywordSearch();
    setAsrKeyword('');
    setAppliedAsrKeyword('');
    setPage(1);
  };

  const clearAsrSearch = () => {
    setAsrMaskTip('正在清除文案筛选，请稍候…');
    setAsrMaskPending(true);
    setAsrKeyword('');
    setAppliedAsrKeyword('');
    setPage(1);
  };

  const onDateChange = (value: Parameters<typeof handleDateChange>[0]) => {
    handleDateChange(value);
    setPage(1);
  };

  const handleNameOrRemarkSave = async (
    record: SourceVideo,
    next: { name?: string; remark?: string }
  ) => {
    const name = (next.name ?? record.name).trim();
    const remark = (next.remark ?? record.remark).trim();

    try {
      const response = await updateSourceVideo(record.id, { name, remark });
      if (response.code !== 0) {
        toast.notify.error(response.message || '保存失败');
        throw new Error(response.message || '保存失败');
      }

      setList((prev) =>
        prev.map((item) =>
          item.id === record.id
            ? { ...item, name: response.data.name, remark: response.data.remark }
            : item
        )
      );
      toast.notify.success(next.name !== undefined ? '名称已修改' : '备注已修改');
    } catch (error) {
      if (error instanceof AppError) {
        showAppError(error);
      } else if (!(error instanceof Error)) {
        toast.notify.error('保存失败');
      }
      // 重新抛出，让 RemarkEditor 还原为修改前内容
      throw error instanceof Error ? error : new Error('保存失败');
    }
  };

  const handleRetryAsr = async (id: number) => {
    setRetryingAsrId(id);
    try {
      const response = await retrySourceVideoAsr(id);
      if (response.code !== 0) {
        toast.notify.error(response.message || '重新解析失败');
        return;
      }

      setList((prev) => prev.map((item) => (item.id === id ? response.data : item)));
      toast.notify.success('已提交重新解析，请稍候');
    } catch (error) {
      if (error instanceof AppError) {
        showAppError(error);
      } else {
        toast.notify.error('重新解析失败');
      }
    } finally {
      setRetryingAsrId(null);
    }
  };

  const handleDelete = async (id: number) => {
    setDeletingId(id);
    try {
      const response = await deleteSourceVideo(id);
      if (response.code !== 0) {
        toast.notify.error(response.message || '删除失败');
        return;
      }

      toast.notify.success('已删除源视频');
      if (list.length === 1 && page > 1) {
        setPage((prev) => prev - 1);
      } else {
        await loadList();
      }
    } catch (error) {
      if (error instanceof AppError) {
        showAppError(error);
      } else {
        toast.notify.error('删除失败');
      }
    } finally {
      setDeletingId(null);
    }
  };

  const columns = useMemo<ColumnsType<SourceVideo>>(() => {
    const allColumns: ColumnsType<SourceVideo> = [
      {
        title: '源视频名称',
        dataIndex: 'name',
        width: 240,
        key: 'name',
        ellipsis: true,
        render: (name: string, record) => (
          <RemarkEditor
            value={name}
            placeholder="添加名称"
            required
            maxLength={64}
            onSave={(value) => handleNameOrRemarkSave(record, { name: value })}
          />
        ),
      },
      {
        title: '备注',
        dataIndex: 'remark',
        key: 'remark',
        render: (remark: string, record) => (
          <RemarkEditor
            value={remark}
            onSave={(value) => handleNameOrRemarkSave(record, { remark: value })}
          />
        ),
      },
      {
        title: '视频URL',
        dataIndex: 'live_url',
        key: 'live_url',
        ellipsis: true,
        render: (url: string) => {
          const text = url?.trim();
          if (!text) return '-';

          const handleCopy = async () => {
            try {
              if (navigator.clipboard?.writeText) {
                await navigator.clipboard.writeText(text);
              } else {
                const textarea = document.createElement('textarea');
                textarea.value = text;
                textarea.style.position = 'fixed';
                textarea.style.opacity = '0';
                document.body.appendChild(textarea);
                textarea.select();
                document.execCommand('copy');
                document.body.removeChild(textarea);
              }
              toast.notify.success('已复制');
            } catch {
              toast.notify.error('复制失败，请手动复制链接');
            }
          };

          return (
            <div className="source-videos-url-cell">
              <EllipsisTooltip
                text={text}
                className="list-page__cell-ellipsis source-videos-url-cell__text"
              />
              <Button
                type="link"
                size="small"
                className="source-videos-url-cell__action"
                icon={<LuCopy size={14} />}
                aria-label="复制视频URL"
                title="复制"
                onClick={() => void handleCopy()}
              />
            </div>
          );
        },
      },
      {
        title: '时长',
        dataIndex: 'duration',
        key: 'duration',
        width: 80,
        align: 'right',
        render: (duration: number) => (duration > 0 ? formatVideoDurationMs(duration) : '-'),
      },
      {
        title: 'ASR解析进度',
        key: 'asr_progress',
        width: 160,
        render: (_, record) => (
          <AsrProgressCell
            status={record.asr_status}
            progress={record.asr_progress}
            errorMessage={record.asr_error_msg}
            retrying={retryingAsrId === record.id}
            onRetry={
              record.asr_status === 'failed'
                ? () => void handleRetryAsr(record.id)
                : undefined
            }
          />
        ),
      },
      {
        title: 'ASR开始时间',
        dataIndex: 'asr_started_at',
        key: 'asr_started_at',
        width: 160,
        align: 'right',
        render: (value: string) => formatToDateTime(value),
      },
      {
        title: 'ASR完成时间',
        dataIndex: 'asr_updated_at',
        key: 'asr_updated_at',
        width: 160,
        align: 'right',
        render: (value: string, record) =>
          record.asr_status === 'completed' ? formatToDateTime(value) : '-',
      },
      {
        title: '创建者',
        dataIndex: 'created_by',
        key: 'created_by',
        width: 100,
        ellipsis: true,
        render: (createdBy: string) => (
          <EllipsisTooltip text={createdBy || '-'} className="list-page__cell-ellipsis" />
        ),
      },
      {
        title: '创建时间',
        dataIndex: 'created_at',
        key: 'created_at',
        width: 160,
        render: (created_at: string) => formatToDateTime(created_at),
      },
      {
        title: '关联项目',
        dataIndex: 'project_count',
        key: 'project_count',
        width: 90,
        align: 'right',
        render: (count: number, record) => {
          const projectCount = Number(count) > 0 ? Math.floor(Number(count)) : 0;
          if (projectCount <= 0) {
            return <span className="source-videos-project-count is-empty">0</span>;
          }

          const keyword = record.name?.trim();
          return (
            <Button
              type="link"
              className="list-page__action-btn source-videos-project-count"
              title={keyword ? `查看「${keyword}」相关项目` : '查看关联项目'}
              onClick={() => {
                navigate(buildSlicesListLink({ keyword: keyword || undefined }));
              }}
            >
              {projectCount}
            </Button>
          );
        },
      },
      {
        title: '操作',
        key: 'actions',
        width: 245,
        fixed: 'right',
        render: (_, record) => {
          const asrDisabledReason = getAsrActionDisabledReason(
            record.asr_status,
            record.asr_error_msg
          );

          return (
            <Space size={8}>
              {renderSliceAction({
                to: buildSourceVideoSliceLink(String(record.id)),
                icon: <LuSparkles size={14} />,
                label: '智能切片',
                disabledReason: asrDisabledReason,
                onNavigate: navigate,
              })}
              {renderSliceAction({
                to: buildManualVideoSliceLink(String(record.id)),
                icon: <LuScissors size={14} />,
                label: '人工切片',
                disabledReason: asrDisabledReason,
                onNavigate: navigate,
              })}
              <Popconfirm
                title="确认删除该源视频？"
                description="删除后不可恢复，仅删除您自己的源视频数据。"
                okText="删除"
                cancelText="取消"
                okButtonProps={{ danger: true, loading: deletingId === record.id }}
                onConfirm={() => void handleDelete(record.id)}
              >
                <Button
                  type="link"
                  danger
                  className="list-page__action-btn"
                  icon={<LuTrash2 size={14} />}
                  loading={deletingId === record.id}
                >
                  删除
                </Button>
              </Popconfirm>
            </Space>
          );
        },
      },
    ];

    const visibleColumns = allColumns.filter((column) => {
      const key = String(column.key ?? '');
      return visibleKeySet.has(key);
    });

    return [
      ...visibleColumns,
      {
        key: '__column_setting__',
        width: 44,
        fixed: 'right',
        align: 'center',
        className: 'table-column-setting-col',
        title: (
          <TableColumnSetting
            items={SOURCE_VIDEOS_COLUMN_SETTINGS}
            value={visibleKeys}
            defaultValue={defaultVisibleKeys}
            onChange={setVisibleKeys}
          />
        ),
        render: () => null,
      },
    ];
  }, [deletingId, defaultVisibleKeys, navigate, retryingAsrId, setVisibleKeys, visibleKeySet, visibleKeys]);

  const hasActiveAdvancedFilters = Boolean(dateRange?.[0] || appliedAsrKeyword);
  const hasActiveFilters = Boolean(appliedKeyword || hasActiveAdvancedFilters);
  const hasAsrKeyword = Boolean(appliedAsrKeyword.trim());
  /** 视频文案搜索/清除较慢：请求中展示表格 mask */
  const tableLoading = loading && (list.length === 0 || hasAsrKeyword || asrMaskPending);
  const tableLoadingProp = tableLoading
    ? hasAsrKeyword || asrMaskPending
      ? { spinning: true as const, tip: asrMaskTip }
      : true
    : false;
  const asrHitKeywords = useMemo(
    () => parseListKeywords(appliedAsrKeyword),
    [appliedAsrKeyword]
  );
  const asrHitExpandedRowKeys = useMemo(
    () =>
      list
        .filter((item) => (item.matched_paragraphs?.length ?? 0) > 0)
        .map((item) => item.id),
    [list]
  );

  const handleTableChange = (pagination: Parameters<typeof handleTablePaginationChange>[0]) => {
    handleTablePaginationChange(pagination, setPage, setPageSize, pageSize);
  };

  return (
    <ListPageLayout
      className="source-videos-page"
      title="源视频管理"
      description="管理直播源视频，支持添加、筛选、备注与删除。"
      toolbar={
        <ListSearchToolbar
          keyword={keyword}
          onKeywordChange={setKeyword}
          keywordPlaceholder="搜索源视频名称 / 备注（支持 关键词A+关键词B）"
          onSearch={applySearch}
          onKeywordClear={clearSearch}
          loading={loading || refreshing}
          onRefresh={() => void loadList({ refresh: true })}
          refreshing={refreshing}
          hasActiveAdvancedFilters={hasActiveAdvancedFilters}
          extra={
            <Button type="primary" icon={<LuPlus size={16} />} onClick={() => setAddOpen(true)}>
              添加源视频
            </Button>
          }
          advanced={
            <>
              <div className="list-page__filter-field">
                <span className="list-page__filter-label">日期范围</span>
                <DatePicker.RangePicker
                  value={dateRange}
                  allowClear
                  placeholder={['开始日期', '结束日期']}
                  onChange={onDateChange}
                />
              </div>
              <div className="list-page__filter-field">
                <span className="list-page__filter-label">视频文案</span>
                <Input.Search
                  allowClear
                  placeholder="搜索已解析文案（支持 关键词A+关键词B）"
                  value={asrKeyword}
                  loading={loading && (hasAsrKeyword || asrMaskPending)}
                  onChange={(event) => setAsrKeyword(event.target.value)}
                  onSearch={applySearch}
                  onClear={clearAsrSearch}
                />
              </div>
            </>
          }
        />
      }
    >
      <ListPageTable<SourceVideo>
        rowKey="id"
        loading={tableLoadingProp}
        columns={columns}
        dataSource={list}
        scrollX={1600}
        expandable={{
          showExpandColumn: false,
          expandedRowKeys: asrHitExpandedRowKeys,
          rowExpandable: (record) => (record.matched_paragraphs?.length ?? 0) > 0,
          expandedRowRender: (record) => (
            <AsrHitsPanel
              key={`${record.id}-${asrHitKeywords.join('\u0001')}`}
              paragraphs={record.matched_paragraphs ?? []}
              keywords={asrHitKeywords}
            />
          ),
        }}
        empty={
          hasActiveFilters
            ? {
              title: '未找到匹配的源视频',
              description: '试试更换关键词，或调整日期范围与视频文案条件',
            }
            : {
              title: '暂无源视频',
              description: '添加源视频后即可进行切片、人工剪辑与 AI 选片',
              tone: 'primary',
              action: (
                <Button type="primary" icon={<LuPlus size={16} />} onClick={() => setAddOpen(true)}>
                  添加源视频
                </Button>
              ),
            }
        }
        pagination={{
          current: page,
          pageSize,
          total,
          ...DEFAULT_TABLE_PAGINATION,
        }}
        onChange={handleTableChange}
      />

      <AddSourceVideoModal
        open={addOpen}
        onClose={() => setAddOpen(false)}
        onSuccess={() => {
          if (page !== 1) {
            setPage(1);
          } else {
            void loadList();
          }
        }}
        onViewExisting={(name) => {
          setAddOpen(false);
          setAsrKeyword('');
          setAppliedAsrKeyword('');
          setKeyword(name);
          setAppliedKeyword(name);
          setPage(1);
        }}
      />
    </ListPageLayout>
  );
};

export default SourceVideosPage;
