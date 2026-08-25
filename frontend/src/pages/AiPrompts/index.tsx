import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Button, Popconfirm } from 'antd';
import type { ColumnsType } from 'antd/es/table';
import { LuPlus, LuTrash2 } from 'react-icons/lu';

import AiPromptFormModal from '~/components/AiPromptFormModal';
import AiPromptPreviewDrawer from '~/components/AiPromptPreviewDrawer';
import PromptContentCell from '~/components/AiPromptPreviewDrawer/PromptContentCell';
import EllipsisTooltip from '~/components/EllipsisTooltip';
import ListPageLayout from '~/components/ListPageLayout';
import ListPageTable from '~/components/ListPageTable';
import ListSearchToolbar from '~/components/ListSearchToolbar';
import RemarkEditor from '~/components/RemarkEditor';
import { useAppSEO } from '~/hooks/useAppSEO';
import { useListFilters } from '~/hooks/useListFilters';
import { AppError } from '~/services/http';
import {
  deleteAiPrompt,
  fetchAiPromptList,
  updateAiPrompt,
  type AiPrompt,
} from '~/services/aiPrompt';
import { formatToDateTime } from '~/utils/date';
import { toApiKeywords } from '~/utils/listKeywords';
import { DEFAULT_TABLE_PAGINATION, handleTablePaginationChange } from '~/utils/table';
import { showAppError, showScopedError, handleRequestError, toast } from '~/utils/toast';
import { G_EmptyStr } from '~/utils/const';

const PROMPTS_LIST_ERROR_SCOPE = 'prompts-list';

const AiPromptsPage = () => {
  useAppSEO({
    title: '提示词管理',
    path: '/prompts',
    robots: 'noindex, nofollow',
  });

  const { keyword, setKeyword, appliedKeyword, applySearch: applyKeywordSearch, clearSearch: clearKeywordSearch } =
    useListFilters();
  const [loading, setLoading] = useState(false);
  const [refreshing, setRefreshing] = useState(false);
  const hasLoadedRef = useRef(false);
  const [list, setList] = useState<AiPrompt[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [pageSize, setPageSize] = useState(10);
  const [deletingId, setDeletingId] = useState<number | null>(null);
  const [formOpen, setFormOpen] = useState(false);
  const [editingPrompt, setEditingPrompt] = useState<AiPrompt | null>(null);
  const [previewPrompt, setPreviewPrompt] = useState<AiPrompt | null>(null);

  const loadList = useCallback(async (options?: { silent?: boolean; refresh?: boolean }) => {
    const silent = options?.silent ?? false;
    const refresh = options?.refresh ?? false;

    if (refresh) {
      setRefreshing(true);
    } else if (!silent) {
      setLoading(true);
    }

    try {
      const response = await fetchAiPromptList({
        keywords: toApiKeywords(appliedKeyword),
        page,
        page_size: pageSize,
      });

      if (response.code !== 0) {
        if (!silent && !refresh) {
          showScopedError(PROMPTS_LIST_ERROR_SCOPE, response.message || '加载提示词列表失败');
        }
        return;
      }

      setList(response.data.list);
      setTotal(response.data.total);
      hasLoadedRef.current = true;
    } catch (error) {
      if (!silent && !refresh) {
        handleRequestError(PROMPTS_LIST_ERROR_SCOPE, error, '加载提示词列表失败');
      }
    } finally {
      if (refresh) {
        setRefreshing(false);
      } else if (!silent) {
        setLoading(false);
      }
    }
  }, [appliedKeyword, page, pageSize]);

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

  const handleRemarkSave = async (record: AiPrompt, remark: string) => {
    try {
      const response = await updateAiPrompt(record.id, {
        name: record.name,
        content: record.content,
        remark,
      });
      if (response.code !== 0) {
        toast.notify.error(response.message || '备注保存失败');
        throw new Error(response.message || '备注保存失败');
      }

      setList((prev) =>
        prev.map((item) =>
          item.id === record.id
            ? { ...item, remark: response.data.remark, updated_at: response.data.updated_at }
            : item
        )
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
      const response = await deleteAiPrompt(id);
      if (response.code !== 0) {
        toast.notify.error(response.message || '删除失败');
        return;
      }

      toast.notify.success('已删除提示词');
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

  const openCreate = () => {
    setEditingPrompt(null);
    setFormOpen(true);
  };

  const openEdit = (prompt: AiPrompt) => {
    setEditingPrompt(prompt);
    setFormOpen(true);
  };

  const handleFormClose = () => {
    setFormOpen(false);
    setEditingPrompt(null);
  };

  const handleFormSuccess = (prompt: AiPrompt) => {
    if (editingPrompt) {
      setList((prev) => prev.map((item) => (item.id === prompt.id ? prompt : item)));
      handleFormClose();
      return;
    }

    handleFormClose();
    if (page !== 1) {
      setPage(1);
    } else {
      void loadList();
    }
  };

  const columns = useMemo<ColumnsType<AiPrompt>>(
    () => [
      {
        title: '名称',
        dataIndex: 'name',
        key: 'name',
        width: 220,
        ellipsis: true,
        render: (name: string, record) =>
          record.is_editable === 1 ? (
            <button
              type="button"
              className="list-page__cell-link"
              onClick={() => openEdit(record)}
            >
              <EllipsisTooltip text={name} className="list-page__cell-ellipsis" />
            </button>
          ) : (
            <EllipsisTooltip text={name} className="list-page__cell-ellipsis" />
          ),
      },
      {
        title: '提示词信息',
        dataIndex: 'content',
        key: 'content',
        ellipsis: true,
        render: (content: string, record) => (
          <PromptContentCell content={content} onView={() => setPreviewPrompt(record)} />
        ),
      },
      {
        title: '备注',
        dataIndex: 'remark',
        key: 'remark',
        width: 260,
        render: (remark: string, record) =>
          record.is_editable === 1 ? (
            <RemarkEditor
              value={remark}
              maxLength={128}
              onSave={(value) => handleRemarkSave(record, value)}
            />
          ) : (
            <EllipsisTooltip text={remark || '—'} className="list-page__cell-ellipsis" />
          ),
      },
      {
        title: '创建者',
        dataIndex: 'created_by',
        key: 'created_by',
        width: 120,
        ellipsis: true,
        render: (createdBy: string) => (
          <EllipsisTooltip text={createdBy || '-'} className="prompts-cell-ellipsis" />
        ),
      },
      {
        title: '创建时间',
        dataIndex: 'created_at',
        key: 'created_at',
        width: 160,
        render: (createdAt: string) => formatToDateTime(createdAt),
      },
      {
        title: '编辑时间',
        dataIndex: 'updated_at',
        key: 'updated_at',
        width: 160,
        render: (updatedAt: string) => formatToDateTime(updatedAt),
      },
      {
        title: '操作',
        key: 'actions',
        width: 95,
        fixed: 'right',
        render: (_, record) =>
          record.is_editable === 1 ? (
            <Popconfirm
              title="确认删除该提示词？"
              description="删除后不可恢复。"
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
          ) : <span className="list-page__cell-empty">{G_EmptyStr}</span>,
      },
    ],
    [deletingId]
  );

  return (
    <ListPageLayout
      className="prompts-page"
      title="提示词管理"
      description="管理 AI 切片使用的提示词，支持添加、编辑、搜索、备注与删除。"
      toolbar={
        <ListSearchToolbar
          keyword={keyword}
          onKeywordChange={setKeyword}
          keywordPlaceholder="搜索名称 / 提示词 / 备注（支持 关键词A+关键词B）"
          onSearch={applySearch}
          onKeywordClear={clearSearch}
          loading={loading || refreshing}
          onRefresh={() => void loadList({ refresh: true })}
          refreshing={refreshing}
          extra={
            <Button type="primary" icon={<LuPlus size={16} />} onClick={openCreate}>
              添加提示词
            </Button>
          }
        />
      }
    >
      <ListPageTable<AiPrompt>
        rowKey="id"
        loading={loading && list.length === 0}
        columns={columns}
        dataSource={list}
        scrollX={1200}
        empty={
          appliedKeyword
            ? {
              title: '未找到匹配的提示词',
              description: '试试更换关键词，或使用 关键词A+关键词B 组合搜索',
            }
            : {
              title: '暂无提示词',
              description: '添加提示词后，可在 AI 切片时快速选用',
              tone: 'primary',
              action: (
                <Button type="primary" icon={<LuPlus size={16} />} onClick={openCreate}>
                  添加提示词
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
        onChange={(pagination) => handleTablePaginationChange(pagination, setPage, setPageSize, pageSize)}
      />

      <AiPromptFormModal
        open={formOpen}
        prompt={editingPrompt}
        onClose={handleFormClose}
        onSuccess={handleFormSuccess}
      />

      <AiPromptPreviewDrawer
        open={Boolean(previewPrompt)}
        prompt={previewPrompt}
        onClose={() => setPreviewPrompt(null)}
        onEdit={(prompt) => {
          setPreviewPrompt(null);
          openEdit(prompt);
        }}
      />
    </ListPageLayout>
  );
};

export default AiPromptsPage;
