import { useLayoutEffect, useRef, useState } from 'react';

const MIN_SCROLL_Y = 240;
const HEADER_RESERVE = 55;
const PAGINATION_RESERVE = 56;
const COMPACT_PAGINATION_WIDTH = 960;

function measureTableContentHeight(el: HTMLElement) {
  const scrollBody = el.querySelector('.ant-table-body') as HTMLElement | null;
  if (scrollBody) {
    return scrollBody.scrollHeight;
  }

  const tbody = el.querySelector('.ant-table-tbody') as HTMLElement | null;
  return tbody?.getBoundingClientRect().height ?? 0;
}

/** 根据表格容器高度计算 Ant Table `scroll.y`；仅内容溢出时启用纵向滚动 */
export function useListTableScrollY(deps: unknown[] = []) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const [scrollY, setScrollY] = useState<number>();
  const [needScroll, setNeedScroll] = useState(false);
  const [compactPagination, setCompactPagination] = useState(false);

  useLayoutEffect(() => {
    const el = wrapRef.current;
    if (!el) return;

    const measure = () => {
      const wrapHeight = el.getBoundingClientRect().height;
      const wrapWidth = el.getBoundingClientRect().width;
      if (wrapHeight <= 0) return;

      setCompactPagination(wrapWidth > 0 && wrapWidth < COMPACT_PAGINATION_WIDTH);

      const tableHeader =
        el.querySelector('.ant-table-thead')?.getBoundingClientRect().height ??
        el.querySelector('.ant-table-header')?.getBoundingClientRect().height ??
        HEADER_RESERVE;

      const paginationEl = el.querySelector('.ant-table-pagination');
      const pagination = paginationEl
        ? paginationEl.getBoundingClientRect().height
        : paginationEl === null && el.querySelector('.ant-table')
          ? 0
          : PAGINATION_RESERVE;

      const maxBodyHeight = wrapHeight - tableHeader - pagination - 2;
      const maxScrollY = Math.max(MIN_SCROLL_Y, Math.floor(maxBodyHeight));
      const contentHeight = measureTableContentHeight(el);

      if (contentHeight > maxScrollY) {
        setNeedScroll(true);
        setScrollY(maxScrollY);
      } else {
        setNeedScroll(false);
        setScrollY(undefined);
      }
    };

    measure();
    const rafId = requestAnimationFrame(measure);

    const ro = new ResizeObserver(() => {
      requestAnimationFrame(measure);
    });
    ro.observe(el);

    const toolbar = el.closest('.list-page')?.querySelector('.list-page__toolbar');
    if (toolbar) ro.observe(toolbar);

    const tbody = el.querySelector('.ant-table-tbody');
    if (tbody) ro.observe(tbody);

    window.addEventListener('resize', measure);

    return () => {
      cancelAnimationFrame(rafId);
      ro.disconnect();
      window.removeEventListener('resize', measure);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);

  return { wrapRef, scrollY, needScroll, compactPagination };
}
