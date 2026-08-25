import { useCallback, useMemo, useState } from 'react';

function getDefaultVisibleKeys(
  columnKeys: string[],
  lockedKeys: string[],
  defaultHiddenKeys: string[]
): string[] {
  const lockedKeySet = new Set(lockedKeys);
  const hidden = new Set(defaultHiddenKeys);
  return columnKeys.filter((key) => lockedKeySet.has(key) || !hidden.has(key));
}

function readStoredKeys(storageKey: string): string[] | null {
  try {
    const raw = localStorage.getItem(storageKey);
    if (!raw) return null;
    const parsed = JSON.parse(raw) as unknown;
    if (!Array.isArray(parsed) || !parsed.every((item) => typeof item === 'string')) {
      return null;
    }
    return parsed;
  } catch {
    return null;
  }
}

export function useTableColumnVisibility(options: {
  storageKey: string;
  /** 全部可配置列 key（顺序即默认展示顺序） */
  columnKeys: string[];
  /** 不可隐藏的列 */
  lockedKeys?: string[];
  /** 默认隐藏的列（仅在无本地缓存时生效） */
  defaultHiddenKeys?: string[];
}) {
  const { storageKey, columnKeys, lockedKeys = [], defaultHiddenKeys = [] } = options;

  const defaultVisibleKeys = useMemo(
    () => getDefaultVisibleKeys(columnKeys, lockedKeys, defaultHiddenKeys),
    [columnKeys, lockedKeys, defaultHiddenKeys]
  );

  const [visibleKeys, setVisibleKeysState] = useState<string[]>(() => {
    const stored = readStoredKeys(storageKey);
    if (stored) {
      const allowed = new Set(columnKeys);
      const next = stored.filter((key) => allowed.has(key));
      for (const key of lockedKeys) {
        if (allowed.has(key) && !next.includes(key)) next.push(key);
      }
      return next.length > 0 ? next : defaultVisibleKeys;
    }
    return defaultVisibleKeys;
  });

  const setVisibleKeys = useCallback(
    (keys: string[]) => {
      const allowed = new Set(columnKeys);
      const next = keys.filter((key) => allowed.has(key));
      for (const key of lockedKeys) {
        if (allowed.has(key) && !next.includes(key)) next.push(key);
      }
      const normalized = next.length > 0 ? next : defaultVisibleKeys;
      setVisibleKeysState(normalized);
      try {
        localStorage.setItem(storageKey, JSON.stringify(normalized));
      } catch {
        // ignore quota / private mode
      }
    },
    [columnKeys, defaultVisibleKeys, lockedKeys, storageKey]
  );

  const visibleKeySet = useMemo(() => new Set(visibleKeys), [visibleKeys]);

  return {
    visibleKeys,
    visibleKeySet,
    setVisibleKeys,
    defaultVisibleKeys,
  };
}
