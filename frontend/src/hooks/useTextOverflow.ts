import { useCallback, useEffect, useRef, useState } from 'react';

/** 检测单行文本是否发生水平溢出（用于省略后展示「查看」等操作） */
export function useTextOverflow(text?: string) {
  const ref = useRef<HTMLDivElement>(null);
  const [overflow, setOverflow] = useState(false);

  const measure = useCallback(() => {
    const el = ref.current;
    if (!el || !text?.trim()) {
      setOverflow(false);
      return;
    }
    setOverflow(el.scrollWidth > el.clientWidth + 1);
  }, [text]);

  useEffect(() => {
    measure();
  }, [measure]);

  useEffect(() => {
    const el = ref.current;
    if (!el || typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(() => measure());
    ro.observe(el);
    return () => ro.disconnect();
  }, [measure]);

  return { ref, overflow };
}
