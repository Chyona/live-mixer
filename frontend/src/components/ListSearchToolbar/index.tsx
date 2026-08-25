import { useState, type ReactNode } from 'react';
import { Button, Input } from 'antd';
import { LuChevronDown, LuRefreshCw, LuSlidersHorizontal } from 'react-icons/lu';

export interface ListSearchToolbarProps {
  keyword: string;
  onKeywordChange: (value: string) => void;
  keywordPlaceholder?: string;
  onSearch: () => void;
  /** 点击清除按钮时：清空关键词并触发无关键词查询 */
  onKeywordClear?: () => void;
  /** 搜索请求进行中（Input.Search 转圈） */
  loading?: boolean;
  /** 刷新当前列表 */
  onRefresh?: () => void;
  refreshing?: boolean;
  /** 高级筛选面板；传入时显示「高级筛选」切换按钮 */
  advanced?: ReactNode;
  /** 是否存在已生效的高级筛选项 */
  hasActiveAdvancedFilters?: boolean;
  defaultAdvancedOpen?: boolean;
  /** 工具栏内联筛选项（如状态选择） */
  inlineFilters?: ReactNode;
  /** 工具栏右侧操作区（如「添加」按钮） */
  extra?: ReactNode;
}

const ListSearchToolbar = ({
  keyword,
  onKeywordChange,
  keywordPlaceholder = '搜索',
  onSearch,
  onKeywordClear,
  onRefresh,
  refreshing = false,
  advanced,
  hasActiveAdvancedFilters = false,
  defaultAdvancedOpen = false,
  inlineFilters,
  extra,
}: ListSearchToolbarProps) => {
  const [advancedOpen, setAdvancedOpen] = useState(defaultAdvancedOpen);

  return (
    <div className="list-page__toolbar-search">
      <div className="list-page__toolbar-main">
        <Input.Search
          className="list-page__search-primary"
          size="large"
          allowClear
          placeholder={keywordPlaceholder}
          value={keyword}
          onChange={(event) => onKeywordChange(event.target.value)}
          onSearch={() => onSearch()}
          onClear={onKeywordClear}
        />

        {inlineFilters}

        {advanced ? (
          <Button
            type="text"
            className={[
              'list-page__advanced-toggle',
              hasActiveAdvancedFilters ? 'list-page__advanced-toggle_active' : '',
            ]
              .filter(Boolean)
              .join(' ')}
            icon={<LuSlidersHorizontal size={14} />}
            onClick={() => setAdvancedOpen((open) => !open)}
          >
            高级筛选
            <LuChevronDown
              size={14}
              className={`list-page__advanced-chevron${advancedOpen ? ' list-page__advanced-chevron_open' : ''}`}
            />
          </Button>
        ) : null}

        {extra || onRefresh ? (
          <div className="list-page__toolbar-actions">
            {onRefresh ? (
              <Button
                type="text"
                className="list-page__refresh-btn"
                icon={<LuRefreshCw size={14} />}
                loading={refreshing}
                aria-label="刷新列表"
                title="刷新"
                onClick={onRefresh}
              >
                刷新
              </Button>
            ) : null}
            {extra ? <div className="list-page__toolbar-extra">{extra}</div> : null}
          </div>
        ) : null}
      </div>

      {advanced && advancedOpen ? (
        <div className="list-page__toolbar-advanced">{advanced}</div>
      ) : null}
    </div>
  );
};

export default ListSearchToolbar;
