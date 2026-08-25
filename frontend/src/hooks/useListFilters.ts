import { useCallback, useMemo, useState } from 'react';
import type { Dayjs } from 'dayjs';
import { buildDateRange } from '~/utils/date';

export function useListFilters(options?: { initialKeyword?: string }) {
  const initialKeyword = options?.initialKeyword?.trim() ?? '';
  const [keyword, setKeyword] = useState(initialKeyword);
  const [appliedKeyword, setAppliedKeyword] = useState(initialKeyword);
  const [dateRange, setDateRange] = useState<[Dayjs | null, Dayjs | null] | null>(null);

  const applySearch = useCallback(() => {
    setAppliedKeyword(keyword.trim());
  }, [keyword]);

  /** 清空关键词并立即生效（用于输入框清除按钮） */
  const clearSearch = useCallback(() => {
    setKeyword('');
    setAppliedKeyword('');
  }, []);

  const handleDateChange = useCallback((value: [Dayjs | null, Dayjs | null] | null) => {
    setDateRange(value);
  }, []);

  const dateFilters = useMemo(() => buildDateRange(dateRange), [dateRange]);

  return {
    keyword,
    setKeyword,
    appliedKeyword,
    setAppliedKeyword,
    applySearch,
    clearSearch,
    dateRange,
    handleDateChange,
    dateFilters,
  };
}
