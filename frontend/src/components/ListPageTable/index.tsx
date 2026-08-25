import { Table } from 'antd';
import type { TableProps } from 'antd';

import ListTableEmpty, { type ListTableEmptyProps } from '~/components/ListTableEmpty';
import { useListTableScrollY } from '~/hooks/useListTableScrollY';
import { buildListPagePagination } from '~/utils/table';

export type ListPageTableProps<T extends object> = Omit<TableProps<T>, 'scroll'> & {
  scrollX?: number | string;
  scroll?: TableProps<T>['scroll'];
  empty?: ListTableEmptyProps;
};

function ListPageTable<T extends object>({
  scrollX,
  scroll,
  className,
  loading,
  dataSource,
  pagination,
  empty,
  locale,
  ...rest
}: ListPageTableProps<T>) {
  const paginationKey =
    pagination === false
      ? false
      : typeof pagination === 'object'
        ? pagination.current ?? pagination.pageSize ?? pagination.total
        : pagination;

  const { wrapRef, scrollY, needScroll, compactPagination } = useListTableScrollY([
    loading,
    dataSource?.length,
    paginationKey,
  ]);

  const resolvedPagination = buildListPagePagination(pagination, {
    compact: compactPagination,
  });

  const isLoading =
    typeof loading === 'object' && loading !== null
      ? Boolean((loading as { spinning?: boolean }).spinning)
      : Boolean(loading);

  const wrapClassName = [
    'list-page__table-wrap',
    needScroll ? 'list-page__table-wrap--scrollable' : '',
    compactPagination ? 'list-page__table-wrap--compact-pagination' : '',
    !isLoading && (dataSource?.length ?? 0) === 0 ? 'list-page__table-wrap--empty' : '',
  ]
    .filter(Boolean)
    .join(' ');

  return (
    <div
      ref={wrapRef}
      className={wrapClassName}
      style={
        needScroll && scrollY !== undefined
          ? ({ '--list-table-body-height': `${scrollY}px` } as React.CSSProperties)
          : undefined
      }
    >
      <Table<T>
        {...rest}
        loading={loading}
        dataSource={dataSource}
        pagination={resolvedPagination}
        locale={{
          ...locale,
          emptyText: <ListTableEmpty {...empty} />,
        }}
        className={['list-page__table', className].filter(Boolean).join(' ')}
        scroll={{
          ...scroll,
          x: scrollX ?? scroll?.x,
          ...(needScroll && scrollY !== undefined ? { y: scrollY } : {}),
        }}
      />
    </div>
  );
}

export default ListPageTable;
