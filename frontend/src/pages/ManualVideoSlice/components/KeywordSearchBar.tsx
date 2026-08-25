import { Input } from 'antd';
import { LuSearch } from 'react-icons/lu';

interface KeywordSearchBarProps {
  value: string;
  onChange: (value: string) => void;
  matchCount: number;
  activeMatchIndex: number;
  onPrevMatch: () => void;
  onNextMatch: () => void;
}

const KeywordSearchBar = ({
  value,
  onChange,
  matchCount,
  activeMatchIndex,
  onPrevMatch,
  onNextMatch,
}: KeywordSearchBarProps) => {
  return (
    <div className="slice-editor-search slice-editor-search_inline">
      <div className="slice-editor-search-row">
        <div className="slice-editor-search-input-wrap">
          <Input
            allowClear
            prefix={<LuSearch size={16} className="slice-editor-search-icon" />}
            value={value}
            placeholder="输入关键词快速定位文案"
            onChange={(event) => onChange(event.target.value)}
            onPressEnter={onNextMatch}
          />
        </div>
        {value.trim() ? (
          <div className="slice-editor-search-meta">
            <span>
              {matchCount > 0 ? `${activeMatchIndex + 1}/${matchCount}` : '无匹配'}
            </span>
            <button type="button" onClick={onPrevMatch} disabled={matchCount === 0}>
              上一个
            </button>
            <button type="button" onClick={onNextMatch} disabled={matchCount === 0}>
              下一个
            </button>
          </div>
        ) : null}
      </div>
    </div>
  );
};

export default KeywordSearchBar;
