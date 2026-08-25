import { Button, Checkbox, Popover } from 'antd';
import type { CheckboxChangeEvent } from 'antd/es/checkbox';
import { LuSettings2 } from 'react-icons/lu';

import './index.css';

export type TableColumnSettingItem = {
  key: string;
  label: string;
  locked?: boolean;
};

type TableColumnSettingProps = {
  items: TableColumnSettingItem[];
  value: string[];
  onChange: (keys: string[]) => void;
  /** 重置时恢复的默认可见列；不传则恢复为全部列 */
  defaultValue?: string[];
};

/** 表头右侧：勾选显示/隐藏列 */
export function TableColumnSetting({
  items,
  value,
  onChange,
  defaultValue,
}: TableColumnSettingProps) {
  const valueSet = new Set(value);

  const handleCheck = (key: string, locked: boolean | undefined) => (event: CheckboxChangeEvent) => {
    if (locked) return;
    if (event.target.checked) {
      onChange([...value, key]);
      return;
    }
    onChange(value.filter((item) => item !== key));
  };

  const handleReset = () => {
    onChange(defaultValue ?? items.map((item) => item.key));
  };

  const content = (
    <div className="table-column-setting">
      <div className="table-column-setting__header">
        <span>列展示</span>
        <Button type="link" size="small" className="table-column-setting__reset" onClick={handleReset}>
          重置
        </Button>
      </div>
      <div className="table-column-setting__list">
        {items.map((item) => (
          <label key={item.key} className="table-column-setting__item">
            <Checkbox
              checked={valueSet.has(item.key)}
              disabled={item.locked}
              onChange={handleCheck(item.key, item.locked)}
            />
            <span className="table-column-setting__label">{item.label}</span>
          </label>
        ))}
      </div>
    </div>
  );

  return (
    <Popover
      trigger="click"
      placement="bottomRight"
      arrow={false}
      content={content}
      overlayClassName="table-column-setting-popover"
    >
      <Button
        type="text"
        size="small"
        className="table-column-setting__trigger"
        icon={<LuSettings2 size={16} />}
        aria-label="配置显示列"
      />
    </Popover>
  );
}

export default TableColumnSetting;
