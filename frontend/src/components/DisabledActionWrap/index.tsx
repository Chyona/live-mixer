import type { ReactNode } from 'react';
import { Tooltip } from 'antd';

interface DisabledActionWrapProps {
  disabledReason: string | null;
  children: ReactNode;
}

const DisabledActionWrap = ({ disabledReason, children }: DisabledActionWrapProps) => {
  if (!disabledReason) {
    return children;
  }

  return (
    <Tooltip title={disabledReason}>
      <span className="disabled-action-wrap">{children}</span>
    </Tooltip>
  );
};

export default DisabledActionWrap;
