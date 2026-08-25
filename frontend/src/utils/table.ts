import type { TablePaginationConfig } from 'antd/es/table';

export const DEFAULT_TABLE_PAGINATION: Pick<
  TablePaginationConfig,
  'showSizeChanger' | 'showTotal' | 'responsive' | 'showLessItems'
> = {
  showSizeChanger: true,
  showTotal: (count) => `共 ${count} 条`,
  responsive: true,
  showLessItems: true,
};

export function buildListPagePagination(
  pagination: TablePaginationConfig | false | undefined,
  options?: { compact?: boolean }
): TablePaginationConfig | false {
  if (pagination === false) return false;

  const compact = options?.compact;

  return {
    ...DEFAULT_TABLE_PAGINATION,
    ...pagination,
    ...(compact
      ? {
          size: 'small',
          showLessItems: true,
          showSizeChanger: false,
        }
      : {}),
  };
}

export function handleTablePaginationChange(
  pagination: TablePaginationConfig,
  setPage: (page: number) => void,
  setPageSize: (pageSize: number) => void,
  currentPageSize: number
) {
  if (pagination.current) {
    setPage(pagination.current);
  }

  if (pagination.pageSize) {
    setPageSize(pagination.pageSize ?? currentPageSize);
  }
}
