import type { ReactNode } from 'react';
import { Breadcrumb } from 'antd';
import type { BreadcrumbProps } from 'antd';
import tipIcon from '~/assets/videos/tip-icon.png';

import './index.css';

export interface SlicePageToolbarProps {
  title: string;
  description?: string;
  actions?: ReactNode;
  tip?: {
    text: string;
    onClick?: () => void;
  };
  className?: string;
}

interface SlicePageHeaderProps extends SlicePageToolbarProps {
  breadcrumbItems: BreadcrumbProps['items'];
}

export const SlicePageBreadcrumb = ({ items }: { items: BreadcrumbProps['items'] }) => (
  <Breadcrumb className="slice-page-breadcrumb" items={items} />
);

export const SlicePageToolbar = ({
  title,
  description,
  actions,
  tip,
  className,
}: SlicePageToolbarProps) => {
  return (
    <div className={['slice-page-header-main', className].filter(Boolean).join(' ')}>
      <div>
        <h1 className="slice-page-title">{title}</h1>
        {description && <p className="slice-page-desc">{description}</p>}
      </div>
      {tip || actions ? (
        <div className="slice-page-header-right">
          {tip ? (
            <button type="button" className="slice-page-tip" onClick={tip.onClick}>
              <span>{tip.text}</span>
              <img src={tipIcon} className="slice-page-tip-icon" alt="提示" />
            </button>
          ) : null}
          {actions ? <div className="slice-page-actions">{actions}</div> : null}
        </div>
      ) : null}
    </div>
  );
};

const SlicePageHeader = ({
  breadcrumbItems,
  title,
  description,
  actions,
  tip,
}: SlicePageHeaderProps) => {
  return (
    <div className="slice-page-header">
      <SlicePageBreadcrumb items={breadcrumbItems} />
      <SlicePageToolbar title={title} description={description} actions={actions} tip={tip} />
    </div>
  );
};

export default SlicePageHeader;
