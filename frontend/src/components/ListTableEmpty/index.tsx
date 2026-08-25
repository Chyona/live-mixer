import type { ReactNode } from 'react';
import { LuInbox } from 'react-icons/lu';

import './index.css';

export type ListTableEmptyTone = 'neutral' | 'primary';

export interface ListTableEmptyProps {
  title?: string;
  description?: string;
  action?: ReactNode;
  tone?: ListTableEmptyTone;
}

const ListTableEmpty = ({
  title = '暂无数据',
  description,
  action,
  tone = 'neutral',
}: ListTableEmptyProps) => {
  return (
    <div className="list-table-empty">
      <div className={`list-table-empty__card list-table-empty__card_${tone}`}>
        <div className={`list-table-empty__icon list-table-empty__icon_${tone}`} aria-hidden>
          <LuInbox size={26} strokeWidth={1.6} />
        </div>
        <p className="list-table-empty__title">{title}</p>
        {description ? <p className="list-table-empty__desc">{description}</p> : null}
        {action ? <div className="list-table-empty__action">{action}</div> : null}
      </div>
    </div>
  );
};

export default ListTableEmpty;
