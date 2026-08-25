import { Button } from 'antd';
import { LuEye } from 'react-icons/lu';

import { useTextOverflow } from '~/hooks/useTextOverflow';

import './index.css';

interface PromptContentCellProps {
  content: string;
  onView: () => void;
}

/** 提示词信息：超长省略，仅溢出时显示查看 */
const PromptContentCell = ({ content, onView }: PromptContentCellProps) => {
  const { ref, overflow } = useTextOverflow(content);

  return (
    <div className="prompts-content-cell">
      <div ref={ref} className="list-page__cell-ellipsis prompts-content-cell__text">
        {content}
      </div>
      {overflow ? (
        <Button
          type="link"
          size="small"
          className="prompts-content-cell__action"
          icon={<LuEye size={14} />}
          aria-label="查看提示词"
          title="点击查看"
          onClick={onView}
        />
      ) : null}
    </div>
  );
};

export default PromptContentCell;
