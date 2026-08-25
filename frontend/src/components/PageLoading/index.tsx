import { Spin } from 'antd';

interface PageLoadingProps {
  className?: string;
  /** 占满视口并垂直居中（路由 Suspense / 整页加载） */
  viewport?: boolean;
}

const PageLoading = ({ className, viewport = false }: PageLoadingProps) => {
  return (
    <div
      className={['page-loading', viewport ? 'page-loading_viewport' : '', className]
        .filter(Boolean)
        .join(' ')}
    >
      <Spin size="large" />
    </div>
  );
};

export default PageLoading;
