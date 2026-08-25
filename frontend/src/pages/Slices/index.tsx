import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, Link, useSearchParams } from 'react-router-dom';
import { Button, DatePicker, Popconfirm, Space } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { LuSquarePen, LuTrash2, LuVideo } from 'react-icons/lu';

import EllipsisTooltip from '~/components/EllipsisTooltip';
import ListPageLayout from '~/components/ListPageLayout';
import ListPageTable from '~/components/ListPageTable';
import ListSearchToolbar from '~/components/ListSearchToolbar';
import RemarkEditor from '~/components/RemarkEditor';
import { useAppSEO } from '~/hooks/useAppSEO';
import { useListFilters } from '~/hooks/useListFilters';
import { buildSliceProjectEditLink, buildTasksListLink } from '~/routes/links';
import { AppError } from '~/services/http';
import {
  deleteSliceProject,
  fetchSliceProjectList,
  getSliceProjectSegmentCount,
  getSliceProjectAspectRatio,
  updateSliceProject,
  type SliceProject,
} from '~/services/sliceProject';
import { formatToDateTime } from '~/utils/date';
import { toApiKeywords } from '~/utils/listKeywords';
import { DEFAULT_TABLE_PAGINATION, handleTablePaginationChange } from '~/utils/table';
import { showAppError, showScopedError, handleRequestError, toast } from '~/utils/toast';

import './index.css';

const SLICES_LIST_ERROR_SCOPE = 'slices-list';

const SlicesPage = () => {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const initialKeyword = searchParams.get('keyword')?.trim() ?? '';

  useAppSEO({
    title: '项目管理',
    path: '/slices',
    robots: 'noindex, nofollow',
  });

  const {
    keyword,
    setKeyword,
    appliedKeyword,
    applySearch: applyKeywordSearch,
    clearSearch: clearKeywordSearch,
    dateRange,
    handleDateChange,
    dateFilters,
  } = useListFilters({ initialKeyword });
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const hasLoadedRef = useRef(false);
  const [list, setList] = useState<SliceProject[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [deletingId, setDeletingId] = useState<number | null>(null);

  const loadList = useCallback(async (options?: { silent?: boolean; refresh?: boolean }) => {
    const silent = options?.silent ?? false;
    const refresh = options?.refresh ?? false;

    if (refresh) {
      setRefreshing(true);
    } else if (!silent) {
      setLoading(true);
    }

    try {
      const response = await fetchSliceProjectList({
        keywords: toApiKeywords(appliedKeyword),
        start_date: dateFilters.date,
        end_date: dateFilters.dateEnd,
        page,
        page_size: pageSize,
      });

      if (response.code !== 0) {
        if (!silent && !refresh) {
          showScopedError(SLICES_LIST_ERROR_SCOPE, response.message || '加载剪辑项目失败');
        }
        return;
      }

      setList(response.data.list);
      setTotal(response.data.total);
      hasLoadedRef.current = true;
    } catch (error) {
      if (!silent && !refresh) {
        handleRequestError(SLICES_LIST_ERROR_SCOPE, error, '加载剪辑项目失败');
      }
    } finally {
      if (refresh) {
        setRefreshing(false);
      } else if (!silent) {
        setLoading(false);
      }
    }
  }, [appliedKeyword, dateFilters, page, pageSize]);

  useEffect(() => {
    void loadList();
  }, [loadList]);

  const applySearch = () => {
    applyKeywordSearch();
    setPage(1);
  };

  const clearSearch = () => {
    clearKeywordSearch();
    setPage(1);
  };

  const onDateChange = (value: Parameters<typeof handleDateChange>[0]) => {
    handleDateChange(value);
    setPage(1);
  };

  const handleProjectNameSave = async (id: number, name: string) => {
    try {
      const response = await updateSliceProject(id, { name });
      if (response.code !== 0) {
        toast.notify.error(response.message || '项目名称保存失败');
        throw new Error(response.message || '项目名称保存失败');
      }

      setList((prev) =>
        prev.map((item) => (item.id === id ? { ...item, name: response.data.name } : item))
      );
      toast.notify.success('项目名称已保存');
    } catch (error) {
      if (error instanceof AppError) {
        showAppError(error);
      } else if (!(error instanceof Error)) {
        toast.notify.error('项目名称保存失败');
      }
      throw error instanceof Error ? error : new Error('项目名称保存失败');
    }
  };

  const handleRemarkSave = async (id: number, remark: string) => {
    try {
      const response = await updateSliceProject(id, { remark });
      if (response.code !== 0) {
        toast.notify.error(response.message || '备注保存失败');
        throw new Error(response.message || '备注保存失败');
      }

      setList((prev) =>
        prev.map((item) => (item.id === id ? { ...item, remark: response.data.remark } : item))
      );
      toast.notify.success('备注已保存');
    } catch (error) {
      if (error instanceof AppError) {
        showAppError(error);
      } else if (!(error instanceof Error)) {
        toast.notify.error('备注保存失败');
      }
      throw error instanceof Error ? error : new Error('备注保存失败');
    }
  };

  const handleDelete = async (id: number) => {
    setDeletingId(id);
    try {
      const response = await deleteSliceProject(id);
      if (response.code !== 0) {
        toast.notify.error(response.message || '删除失败');
        return;
      }

      toast.notify.success('已删除剪辑项目');
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

  const hasActiveFilters = Boolean(appliedKeyword || dateRange?.[0]);

  const columns = useMemo<ColumnsType<SliceProject>>(
    () => [
      {
        title: '项目名称',
        dataIndex: 'name',
        key: 'name',
        render: (name: string, record) => (
          <RemarkEditor
            value={name}
            placeholder="输入项目名称"
            required
            onSave={(value) => handleProjectNameSave(record.id, value)}
          />
        ),
      },
      {
        title: '源视频名称',
        dataIndex: 'live_name',
        key: 'live_name',
        ellipsis: true,
        render: (name: string) => (
          <EllipsisTooltip text={name || '-'} className="list-page__cell-ellipsis" />
        ),
      },
      {
        title: '备注',
        dataIndex: 'remark',
        key: 'remark',
        render: (remark: string, record) => (
          <RemarkEditor
            value={remark}
            placeholder="添加备注"
            onSave={(value) => handleRemarkSave(record.id, value)}
          />
        ),
      },
      {
        title: '创建者',
        dataIndex: 'created_by',
        key: 'created_by',
        width: 120,
        ellipsis: true,
        render: (createdBy: string) => (
          <EllipsisTooltip text={createdBy || '-'} className="list-page__cell-ellipsis" />
        ),
      },
      {
        title: '片段数',
        key: 'segment_count',
        width: 90,
        align: 'center',
        render: (_, record) => getSliceProjectSegmentCount(record),
      },
      {
        title: '关联任务',
        dataIndex: 'task_count',
        key: 'task_count',
        width: 90,
        align: 'right',
        render: (count: number, record) => {
          const taskCount = Number(count) > 0 ? Math.floor(Number(count)) : 0;
          if (taskCount <= 0) {
            return <span className="slices-task-count is-empty">0</span>;
          }

          const keyword = record.name?.trim();
          return (
            <Button
              type="link"
              className="list-page__action-btn slices-task-count"
              title={keyword ? `查看「${keyword}」相关任务` : '查看关联任务'}
              onClick={() => {
                navigate(buildTasksListLink({ keyword: keyword || undefined }));
              }}
            >
              {taskCount}
            </Button>
          );
        },
      },
      {
        title: '视频比例',
        key: 'aspect_ratio',
        width: 100,
        align: 'center',
        render: (_, record) => getSliceProjectAspectRatio(record),
      },
      {
        title: '更新时间',
        dataIndex: 'updated_at',
        key: 'updated_at',
        width: 160,
        render: (value: string) => formatToDateTime(value),
      },
      {
        title: '操作',
        key: 'actions',
        width: 150,
        fixed: 'right',
        render: (_, record) => (
          <Space size={8}>
            <Button
              type="link"
              size="small"
              className="list-page__action-btn"
              icon={<LuSquarePen size={14} />}
              onClick={() =>
                navigate(
                  buildSliceProjectEditLink({
                    liveId: record.live_id,
                    id: record.id,
                    projectSource: record.project_source,
                  }),
                  {
                    state: { from: 'slices' },
                  }
                )
              }
            >
              编辑
            </Button>
            <Popconfirm
              title="确认删除该剪辑项目？"
              description="删除后不可恢复"
              okText="删除"
              cancelText="取消"
              okButtonProps={{ danger: true, loading: deletingId === record.id }}
              onConfirm={() => void handleDelete(record.id)}
            >
              <Button
                type="link"
                size="small"
                danger
                className="list-page__action-btn"
                icon={<LuTrash2 size={14} />}
                loading={deletingId === record.id}
              >
                删除
              </Button>
            </Popconfirm>
          </Space>
        ),
      },
    ],
    [deletingId, navigate]
  );

  return (
    <ListPageLayout
      className="slices-page"
      title="项目管理"
      description="管理每个源视频对应的剪辑项目，单视频对应一个可二次编辑的切片项目。"
      toolbar={
        <ListSearchToolbar
          keyword={keyword}
          onKeywordChange={setKeyword}
          keywordPlaceholder="搜索项目名称 / 源视频名称 / 备注（支持 关键词A+关键词B）"
          onSearch={applySearch}
          onKeywordClear={clearSearch}
          loading={loading || refreshing}
          onRefresh={() => void loadList({ refresh: true })}
          refreshing={refreshing}
          hasActiveAdvancedFilters={Boolean(dateRange?.[0])}
          advanced={
            <div className="list-page__filter-field">
              <span className="list-page__filter-label">日期范围</span>
              <DatePicker.RangePicker
                value={dateRange}
                allowClear
                placeholder={['开始日期', '结束日期']}
                onChange={onDateChange}
              />
            </div>
          }
        />
      }
    >
      <ListPageTable<SliceProject>
        rowKey="id"
        loading={loading && list.length === 0}
        columns={columns}
        dataSource={list}
        scrollX={1300}
        empty={
          hasActiveFilters
            ? {
              title: '未找到匹配的剪辑项目',
              description: '试试更换关键词或调整日期范围后重新搜索',
            }
            : {
              title: '暂无剪辑项目',
              description: '在源视频中完成切片后，对应项目会自动汇总到这里',
              tone: 'primary',
              action: (
                <Link to="/source-videos">
                  <Button type="primary" icon={<LuVideo size={16} />}>
                    前往源视频管理
                  </Button>
                </Link>
              ),
            }
        }
        pagination={{
          current: page,
          pageSize,
          total,
          ...DEFAULT_TABLE_PAGINATION,
        }}
        onChange={(pagination) => handleTablePaginationChange(pagination, setPage, setPageSize, pageSize)}
      />
    </ListPageLayout>
  );
};

export default SlicesPage;
