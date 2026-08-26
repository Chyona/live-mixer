import type { ReactNode } from 'react';
import { Breadcrumb } from 'antd';
import type { BreadcrumbProps } from 'antd';
import tipIcon from '~/assets/videos/tip-icon.png';
import CopyableText from '~/components/CopyableText';
import { formatSliceProjectContent } from '~/services/sliceProject';

import './index.css';

export interface SlicePageToolbarProps {
  title: string;
  description?: string;
  extra?: ReactNode;
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

export function SliceProjectMetaBar({
  title,
  description,
  topics,
}: {
  title?: string;
  description?: string;
  topics?: string[];
}) {
  const projectTitle = title?.trim() ?? '';
  const content = formatSliceProjectContent(description, topics);
  if (!projectTitle && !content) return null;

  return (
    <div className="slice-project-meta">
      {projectTitle ? (
        <div className="slice-project-meta__item slice-project-meta__item_title">
          <span className="slice-project-meta__label">标题：</span>
          <CopyableText text={projectTitle} successMessage="标题已复制" />
        </div>
      ) : null}
      {content ? (
        <div className="slice-project-meta__item slice-project-meta__item_content">
          <span className="slice-project-meta__label">内容：</span>
          <CopyableText text={content} successMessage="内容已复制" />
        </div>
      ) : null}
    </div>
  );
}

export const SlicePageToolbar = ({
  title,
  description,
  extra,
  actions,
  tip,
  className,
}: SlicePageToolbarProps) => {
  const hasExtra = Boolean(extra);
  return (
    <div
      className={[
        'slice-page-header-main',
        hasExtra ? 'slice-page-header-main_with-extra' : '',
        className,
      ]
        .filter(Boolean)
        .join(' ')}
    >
      <div className="slice-page-header-left">
        <h1 className="slice-page-title">{title}</h1>
        {description && <p className="slice-page-desc">{description}</p>}
      </div>
      {hasExtra ? <div className="slice-page-header-extra">{extra}</div> : null}
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
  extra,
  actions,
  tip,
}: SlicePageHeaderProps) => {
  return (
    <div className="slice-page-header">
      <SlicePageBreadcrumb items={breadcrumbItems} />
      <SlicePageToolbar
        title={title}
        description={description}
        extra={extra}
        actions={actions}
        tip={tip}
      />
    </div>
  );
};

export default SlicePageHeader;
