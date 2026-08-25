import type { ReactNode } from 'react';

interface ListPageLayoutProps {
  title: string;
  description?: ReactNode;
  action?: ReactNode;
  toolbar?: ReactNode;
  children: ReactNode;
  className?: string;
}

const ListPageLayout = ({
  title,
  description,
  action,
  toolbar,
  children,
  className,
}: ListPageLayoutProps) => {
  return (
    <div className={['list-page', className].filter(Boolean).join(' ')}>
      <div className="list-page__header">
        <div className="list-page__header-main">
          <h1 className="list-page__title">{title}</h1>
          {description ? <p className="list-page__desc">{description}</p> : null}
        </div>
        {action ? <div className="list-page__action">{action}</div> : null}
      </div>

      <div className="list-page__content-card">
        {toolbar ? <div className="list-page__toolbar">{toolbar}</div> : null}
        <div className="list-page__body">{children}</div>
      </div>
    </div>
  );
};

export default ListPageLayout;
