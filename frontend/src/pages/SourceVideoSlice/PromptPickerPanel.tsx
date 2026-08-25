import { Button, Input, Radio, Spin } from 'antd';
import { useCallback, useEffect, useRef, useState, type MouseEvent } from 'react';
import { LuEye, LuMessageSquarePlus, LuPlus, LuSearch, LuSquarePen } from 'react-icons/lu';

import AiPromptFormModal from '~/components/AiPromptFormModal';
import AiPromptPreviewDrawer from '~/components/AiPromptPreviewDrawer';
import EllipsisTooltip from '~/components/EllipsisTooltip';
import { AppError } from '~/services/http';
import { fetchAiPromptList, type AiPrompt } from '~/services/aiPrompt';
import { useTextOverflow } from '~/hooks/useTextOverflow';
import { toApiKeywords } from '~/utils/listKeywords';
import { showAppError, toast } from '~/utils/toast';

import './PromptPickerPanel.css';

const PAGE_SIZE = 8;

/** 无项目回显时，优先选中系统「默认提示词」 */
function pickDefaultPrompt(list: AiPrompt[]): AiPrompt | undefined {
  if (!list.length) return undefined;

  const byExactName = list.find((item) => item.name.trim() === '默认提示词');
  if (byExactName) return byExactName;

  // 系统内置通常不可编辑
  const systemItem = list.find((item) => item.is_editable === 0);
  if (systemItem) return systemItem;

  return list[0];
}

function PromptItemContent({
  name,
  content,
  onView,
}: {
  name: string;
  content: string;
  onView: (event: MouseEvent) => void;
}) {
  const { ref, overflow } = useTextOverflow(content);

  return (
    <div className="slice-prompt-panel__item-content-row">
      <div ref={ref} className="slice-prompt-panel__item-content">
        {content}
      </div>
      {overflow ? (
        <button
          type="button"
          className="slice-prompt-panel__view-btn"
          aria-label={`查看 ${name}`}
          title="查看"
          onClick={onView}
        >
          <LuEye size={13} />
        </button>
      ) : null}
    </div>
  );
}

interface PromptPickerPanelProps {
  selectedId: number | null;
  /** 编辑回显：优先选中该 id（若不在首屏则继续分页查找） */
  preferredId?: number | null;
  onSelect: (prompt: AiPrompt) => void;
  /** 只读：不可新增/编辑/切换提示词 */
  readOnly?: boolean;
}

const PromptPickerPanel = ({
  selectedId,
  preferredId = null,
  onSelect,
  readOnly = false,
}: PromptPickerPanelProps) => {
  const [keyword, setKeyword] = useState('');
  const [appliedKeyword, setAppliedKeyword] = useState('');
  const [list, setList] = useState<AiPrompt[]>([]);
  const [total, setTotal] = useState(0);
  const [page, setPage] = useState(1);
  const [loading, setLoading] = useState(false);
  const [loadingMore, setLoadingMore] = useState(false);
  const [formOpen, setFormOpen] = useState(false);
  const [editingPrompt, setEditingPrompt] = useState<AiPrompt | null>(null);
  const [previewPrompt, setPreviewPrompt] = useState<AiPrompt | null>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const loadingRef = useRef(false);

  const hasMore = list.length < total;

  const loadPage = useCallback(
    async (nextPage: number, keywordValue: string, append: boolean) => {
      if (loadingRef.current) return;

      loadingRef.current = true;
      if (append) {
        setLoadingMore(true);
      } else {
        setLoading(true);
      }

      try {
        const response = await fetchAiPromptList({
          keywords: toApiKeywords(keywordValue),
          page: nextPage,
          page_size: PAGE_SIZE,
        });

        if (response.code !== 0) {
          toast.notify.error(response.message || '加载提示词失败');
          return;
        }

        setTotal(response.data.total);
        setPage(nextPage);
        setList((prev) => (append ? [...prev, ...response.data.list] : response.data.list));
      } catch (error) {
        if (error instanceof AppError) {
          showAppError(error);
        } else {
          toast.notify.error('加载提示词失败');
        }
      } finally {
        loadingRef.current = false;
        setLoading(false);
        setLoadingMore(false);
      }
    },
    []
  );

  const reload = useCallback(() => {
    void loadPage(1, appliedKeyword, false);
  }, [appliedKeyword, loadPage]);

  useEffect(() => {
    void loadPage(1, appliedKeyword, false);
  }, [appliedKeyword, loadPage]);

  useEffect(() => {
    if (selectedId || list.length === 0) return;

    if (preferredId) {
      const matched = list.find((item) => item.id === preferredId);
      if (matched) {
        onSelect(matched);
        return;
      }
      if (hasMore && !loading && !loadingMore) {
        void loadPage(page + 1, appliedKeyword, true);
        return;
      }
      if (hasMore || loading || loadingMore) return;
    }

    const defaultItem = pickDefaultPrompt(list);
    if (defaultItem) {
      onSelect(defaultItem);
    }
  }, [
    appliedKeyword,
    hasMore,
    list,
    loadPage,
    loading,
    loadingMore,
    onSelect,
    page,
    preferredId,
    selectedId,
  ]);

  const handleSearch = () => {
    setAppliedKeyword(keyword.trim());
  };

  const handleClearSearch = () => {
    setKeyword('');
    setAppliedKeyword('');
  };

  const handleLoadMore = useCallback(() => {
    if (!hasMore || loading || loadingMore) return;
    void loadPage(page + 1, appliedKeyword, true);
  }, [appliedKeyword, hasMore, loadPage, loading, loadingMore, page]);

  useEffect(() => {
    const container = listRef.current;
    if (!container) return;

    const handleScroll = () => {
      const distance = container.scrollHeight - container.scrollTop - container.clientHeight;
      if (distance <= 48) {
        handleLoadMore();
      }
    };

    container.addEventListener('scroll', handleScroll);
    return () => container.removeEventListener('scroll', handleScroll);
  }, [handleLoadMore]);

  const handleFormSuccess = (prompt: AiPrompt) => {
    if (editingPrompt) {
      setList((prev) => prev.map((item) => (item.id === prompt.id ? prompt : item)));
      if (selectedId === prompt.id) {
        onSelect(prompt);
      }
      return;
    }

    reload();
    onSelect(prompt);
  };

  const openCreate = () => {
    setEditingPrompt(null);
    setFormOpen(true);
  };

  const openEdit = (prompt: AiPrompt, event?: MouseEvent) => {
    event?.preventDefault();
    event?.stopPropagation();
    setPreviewPrompt(null);
    setEditingPrompt(prompt);
    setFormOpen(true);
  };

  const openPreview = (prompt: AiPrompt, event: MouseEvent) => {
    event.preventDefault();
    event.stopPropagation();
    setPreviewPrompt(prompt);
  };

  const renderEmpty = () => {
    if (appliedKeyword) {
      return (
        <div className="slice-prompt-panel__empty">
          <div className="slice-prompt-panel__empty-icon slice-prompt-panel__empty-icon_neutral" aria-hidden>
            <LuSearch size={22} strokeWidth={1.6} />
          </div>
          <p className="slice-prompt-panel__empty-title">未找到匹配的提示词</p>
          <p className="slice-prompt-panel__empty-desc">
            试试更换关键词，或使用 关键词A+关键词B 组合搜索
          </p>
          <div className="slice-prompt-panel__empty-actions">
            <Button size="small" onClick={handleClearSearch}>
              清除搜索
            </Button>
          </div>
        </div>
      );
    }

    return (
      <div className="slice-prompt-panel__empty">
        <div className="slice-prompt-panel__empty-icon" aria-hidden>
          <LuMessageSquarePlus size={22} strokeWidth={1.6} />
        </div>
        <p className="slice-prompt-panel__empty-title">暂无可用提示词</p>
        <p className="slice-prompt-panel__empty-desc">
          成片前需先选择提示词，新建后即可在此快速选用
        </p>
        <div className="slice-prompt-panel__empty-actions">
          <Button
            type="primary"
            size="middle"
            icon={<LuPlus size={14} />}
            onClick={openCreate}
            disabled={readOnly}
          >
            新增提示词
          </Button>
        </div>
      </div>
    );
  };

  return (
    <aside className="slice-prompt-panel">
      <div className="slice-prompt-panel__header">
        <div className="slice-prompt-panel__header-main">
          <h3 className="slice-prompt-panel__title">AI 提示词</h3>
          <p className="slice-prompt-panel__desc">选择用于成片的提示词</p>
        </div>

        <div className="slice-prompt-panel__header-actions">
          <Input.Search
            className="slice-prompt-panel__search"
            size="middle"
            allowClear
            // loading={loading && !loadingMore}
            placeholder="搜索名称 / 提示词"
            value={keyword}
            onChange={(event) => setKeyword(event.target.value)}
            onSearch={(_value, _event, info) => {
              if (info?.source === 'clear') {
                handleClearSearch();
                return;
              }
              handleSearch();
            }}
            onClear={handleClearSearch}
          />
          <Button type="primary" size="middle" icon={<LuPlus size={14} />} onClick={openCreate} disabled={readOnly}>
            新增
          </Button>
        </div>
      </div>

      <div ref={listRef} className="slice-prompt-panel__list">
        {loading && list.length === 0 ? (
          <div className="slice-prompt-panel__loading" aria-busy="true" aria-label="提示词加载中">
            <Spin size="default" />
            <p className="slice-prompt-panel__loading-text">正在加载提示词…</p>
          </div>
        ) : list.length === 0 ? (
          renderEmpty()
        ) : (
          <Radio.Group
            className="slice-prompt-panel__radio-group"
            value={selectedId ?? undefined}
            onChange={(event) => {
              if (readOnly) return;
              const next = list.find((item) => item.id === event.target.value);
              if (next) onSelect(next);
            }}
          >
            {list.map((item) => (
              <label
                key={item.id}
                className={`slice-prompt-panel__item${selectedId === item.id ? ' active' : ''}`}
              >
                <Radio value={item.id} className="slice-prompt-panel__radio" disabled={readOnly} />
                <div className="slice-prompt-panel__item-body">
                  <div className="slice-prompt-panel__item-head">
                    <div className="slice-prompt-panel__item-name-wrap">
                      <EllipsisTooltip
                        text={item.name}
                        className="slice-prompt-panel__item-name"
                        placement="topLeft"
                      />
                    </div>
                    {item.is_editable === 1 && !readOnly ? (
                      <button
                        type="button"
                        className="slice-prompt-panel__edit-btn"
                        aria-label={`编辑 ${item.name}`}
                        title="编辑"
                        onClick={(event) => openEdit(item, event)}
                      >
                        <LuSquarePen size={13} />
                      </button>
                    ) : null}
                  </div>
                  <PromptItemContent
                    name={item.name}
                    content={item.content}
                    onView={(event) => openPreview(item, event)}
                  />
                </div>
              </label>
            ))}
          </Radio.Group>
        )}

        {loadingMore ? (
          <div className="slice-prompt-panel__loading slice-prompt-panel__loading_more">
            <Spin size="small" />
            <span>加载更多...</span>
          </div>
        ) : null}

        {!loading && list.length > 0 && !hasMore ? (
          <div className="slice-prompt-panel__end">已加载全部 {total} 条</div>
        ) : null}
      </div>

      <AiPromptFormModal
        open={formOpen}
        prompt={editingPrompt}
        onClose={() => {
          setFormOpen(false);
          setEditingPrompt(null);
        }}
        onSuccess={handleFormSuccess}
      />

      <AiPromptPreviewDrawer
        open={previewPrompt != null}
        prompt={previewPrompt}
        onClose={() => setPreviewPrompt(null)}
        onEdit={(prompt) => openEdit(prompt)}
      />
    </aside>
  );
};

export default PromptPickerPanel;
