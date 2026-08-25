import { Tooltip } from 'antd';
import type { ReactNode } from 'react';
import { useCallback, useEffect, useRef, useState } from 'react';

export interface EllipsisTooltipProps {
  /** 展示与测量的文本 */
  text?: string;
  className?: string;
  /** 悬停提示内容，默认与 text 相同 */
  title?: ReactNode;
  /** 为 true 时始终显示 Tooltip（不检测溢出） */
  forceTooltip?: boolean;
  /** 透传给 Tooltip 的 overlayClassName */
  overlayClassName?: string;
  /** Tooltip 弹出位置 */
  placement?: 'top' | 'bottom' | 'left' | 'right' | 'topLeft' | 'topRight' | 'bottomLeft' | 'bottomRight';
}

/**
 * 单行省略场景：默认仅当文本水平溢出时显示 Tooltip；可 forceTooltip 强制显示。
 */
export function EllipsisTooltip({
  text,
  className,
  title,
  forceTooltip = false,
  overlayClassName,
  placement = 'topLeft',
}: EllipsisTooltipProps) {
  const textRef = useRef<HTMLDivElement>(null);
  const [overflow, setOverflow] = useState(false);

  const measure = useCallback(() => {
    const el = textRef.current;
    if (!el || !text?.trim()) {
      setOverflow(false);
      return;
    }
    setOverflow(el.scrollWidth > el.clientWidth);
  }, [text]);

  useEffect(() => {
    measure();
  }, [measure]);

  useEffect(() => {
    const el = textRef.current;
    if (!el || typeof ResizeObserver === 'undefined') return;
    const ro = new ResizeObserver(() => measure());
    ro.observe(el);
    return () => ro.disconnect();
  }, [measure]);

  const inner = (
    <div ref={textRef} className={className}>
      {text}
    </div>
  );

  if (!text?.trim()) {
    return <div className={className} />;
  }

  const tipTitle = title !== undefined ? title : text;
  const showTooltip = forceTooltip || overflow;

  return showTooltip ? (
    <Tooltip
      title={tipTitle}
      placement={placement}
      overlayClassName={overlayClassName}
      mouseEnterDelay={0.25}
    >
      {inner}
    </Tooltip>
  ) : (
    inner
  );
}

export default EllipsisTooltip;
