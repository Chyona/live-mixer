import { Input, Tooltip } from 'antd';
import type { SearchProps } from 'antd/es/input/Search';
import { buildKeywordSearchPlaceholder } from '~/utils/listKeywords';

export interface KeywordSearchInputProps extends SearchProps {
  /** 搜索范围说明，会与语法说明拼成 placeholder / 悬浮提示 */
  scopePlaceholder?: string;
}

const KeywordSearchInput = ({
  scopePlaceholder = '搜索',
  placeholder,
  value,
  defaultValue,
  ...rest
}: KeywordSearchInputProps) => {
  const resolvedPlaceholder = placeholder ?? buildKeywordSearchPlaceholder(scopePlaceholder);
  const currentValue = value ?? defaultValue;
  const showHint =
    currentValue === undefined || currentValue === null || String(currentValue) === '';

  return (
    <Tooltip
      title={showHint ? resolvedPlaceholder : undefined}
      mouseEnterDelay={0.3}
      placement="topLeft"
      classNames={{ root: 'keyword-search-input-tooltip' }}
      styles={{
        root: {
          width: 'max-content',
          maxWidth: 'none',
        },
        body: {
          width: 'max-content',
          maxWidth: 'none',
          whiteSpace: 'nowrap',
          wordWrap: 'normal',
          overflowWrap: 'normal',
        },
      }}
    >
      <span className="keyword-search-input-wrap">
        <Input.Search
          placeholder={resolvedPlaceholder}
          value={value}
          defaultValue={defaultValue}
          {...rest}
        />
      </span>
    </Tooltip>
  );
};

export default KeywordSearchInput;
