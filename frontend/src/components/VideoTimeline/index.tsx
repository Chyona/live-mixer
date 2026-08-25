import { useState, useRef, useCallback, useEffect, type CSSProperties, type FC, useMemo } from 'react';
import { toast } from '~/utils/toast';
import './index.css';

export interface TimeRange {
  id: string;
  start: number;
  end: number;
  /** 片段展示名，来自 asr_summaries.title 等 */
  title?: string;
}

export interface VideoTimelineProps {
  duration: number;
  currentTime: number;
  selectedRanges: TimeRange[];
  /** AI 选片预选：仅展示，点击可复制为已选片段 */
  previewRanges?: TimeRange[];
  maxTotalDuration?: number; // 选中总时长上限（秒）
  zoomLevel?: number;
  onZoomLevelChange?: (level: number) => void;
  activeRangeId?: string | null;
  onActiveRangeChange?: (id: string | null) => void;
  onTimeChange: (time: number) => void;
  onRangeSelect: (range: TimeRange) => void;
  onRangeDelete: (id: string) => void;
  onRangeUpdate?: (range: TimeRange) => void;
  /** 点击 AI 预选片段（父级负责复制为已选） */
  onPreviewRangeClick?: (range: TimeRange) => void;
  /** 只读：可定位播放，不可新增/调整/删除片段 */
  readOnly?: boolean;
}

function formatTime(seconds: number): string {
  const hrs = Math.floor(seconds / 3600);
  const mins = Math.floor((seconds % 3600) / 60);
  const secs = Math.floor(seconds % 60);
  if (hrs > 0) {
    return `${hrs}:${String(mins).padStart(2, '0')}:${String(secs).padStart(2, '0')}`;
  }
  return `${mins}:${String(secs).padStart(2, '0')}`;
}

function formatTimeMs(seconds: number): string {
  const hrs = Math.floor(seconds / 3600);
  const mins = Math.floor((seconds % 3600) / 60);
  const secs = Math.floor(seconds % 60);
  const ms = Math.floor((seconds % 1) * 100);
  if (hrs > 0) {
    return `${hrs}:${String(mins).padStart(2, '0')}:${String(secs).padStart(2, '0')}.${String(ms).padStart(2, '0')}`;
  }
  return `${mins}:${String(secs).padStart(2, '0')}.${String(ms).padStart(2, '0')}`;
}

const MIN_TICK_SPACING_PX = 50;
const TIMELINE_EDGE_GUTTER = 20;
const TIMELINE_LABEL_EDGE_THRESHOLD = 40;
const NARROW_RANGE_DELETE_WIDTH = 44;
const MIN_RANGE_DURATION = 1;

function toTimelineX(time: number, scale: number) {
  return TIMELINE_EDGE_GUTTER + time * scale;
}

function fromTimelineX(x: number, scale: number) {
  return (x - TIMELINE_EDGE_GUTTER) / scale;
}

type RangeSegmentType = 'core' | 'added' | 'removed';

interface RangeSegment {
  type: RangeSegmentType;
  start: number;
  end: number;
}

function buildRangeVisual(
  original: TimeRange,
  preview?: { start: number; end: number } | null
): { containerStart: number; containerEnd: number; previewStart: number; previewEnd: number; segments: RangeSegment[] } {
  if (!preview) {
    return {
      containerStart: original.start,
      containerEnd: original.end,
      previewStart: original.start,
      previewEnd: original.end,
      segments: [{ type: 'core', start: original.start, end: original.end }],
    };
  }

  const { start: oStart, end: oEnd } = original;
  const { start: pStart, end: pEnd } = preview;
  const segments: RangeSegment[] = [];

  if (pStart > oStart) {
    segments.push({ type: 'removed', start: oStart, end: pStart });
  }
  if (pEnd < oEnd) {
    segments.push({ type: 'removed', start: pEnd, end: oEnd });
  }

  const coreStart = Math.max(oStart, pStart);
  const coreEnd = Math.min(oEnd, pEnd);
  if (coreEnd > coreStart) {
    segments.push({ type: 'core', start: coreStart, end: coreEnd });
  }

  if (pStart < oStart) {
    segments.push({ type: 'added', start: pStart, end: oStart });
  }
  if (pEnd > oEnd) {
    segments.push({ type: 'added', start: oEnd, end: pEnd });
  }

  return {
    containerStart: Math.min(oStart, pStart),
    containerEnd: Math.max(oEnd, pEnd),
    previewStart: pStart,
    previewEnd: pEnd,
    segments,
  };
}

interface Tick {
  time: number;
  isMajor: boolean;
  label: string;
  isSub?: boolean;
}

function getTickConfig(duration: number, scale: number, wrapperWidth: number) {
  if (!Number.isFinite(duration) || duration <= 0 || !Number.isFinite(scale) || scale <= 0) {
    return { tickInterval: 1, subInterval: 0 };
  }

  const intervals = [0.5, 1, 5, 10, 15, 30, 60, 120, 300, 600, 900, 1800, 3600];
  const idealInterval = MIN_TICK_SPACING_PX / scale;
  let tickInterval = intervals[intervals.length - 1]!;
  for (const interval of intervals) {
    if (interval >= idealInterval) {
      tickInterval = interval;
      break;
    }
  }

  // 根据视频时长动态调整：确保视野内（约1200px）主刻度数量在 8~40 之间
  const visibleTime = 1200 / scale;
  const maxVisibleTicks = 40;
  const minVisibleTicks = 8;
  while (visibleTime / tickInterval > maxVisibleTicks) {
    const idx = intervals.indexOf(tickInterval);
    if (idx < 0 || idx >= intervals.length - 1) break;
    tickInterval = intervals[idx + 1]!;
  }
  // 如果视野内刻度太少（放大时），允许使用更小的间隔
  while (visibleTime / tickInterval < minVisibleTicks && tickInterval > 0.05) {
    const idx = intervals.indexOf(tickInterval);
    if (idx <= 0) break;
    tickInterval = intervals[idx - 1]!;
  }

  // 主刻度内的小刻度；优先密，像素不够时逐步变疏
  const subDivisions: Record<number, number> = {
    0.2: 0.05,
    0.5: 0.1,
    1: 0.2,
    5: 1,
    10: 2,
    15: 5,
    30: 5,
    60: 10,
    120: 30,
    300: 60,
    600: 120,
    900: 180,
    1800: 300,
    3600: 600,
  };

  const minSubTickSpacingPx = 8;
  const preferredSub = subDivisions[tickInterval] ?? 0;
  let subInterval = 0;
  if (preferredSub > 0) {
    const candidates = [preferredSub, preferredSub * 2, preferredSub * 3, tickInterval / 2].filter(
      (v, i, arr) => v > 0 && v < tickInterval && arr.indexOf(v) === i
    );
    for (const candidate of candidates) {
      if (candidate * scale >= minSubTickSpacingPx) {
        subInterval = candidate;
        break;
      }
    }
  }

  return { tickInterval, subInterval };
}

function generateVisibleTicks(
  duration: number,
  tickInterval: number,
  subInterval: number,
  safeScale: number,
  scrollLeft: number,
  wrapperWidth: number,
  edgeGutter: number
): Tick[] {
  if (!Number.isFinite(duration) || duration <= 0 || tickInterval <= 0) {
    return [];
  }

  const buffer = 100; // 像素缓冲区
  const startTime = Math.max(0, (scrollLeft - edgeGutter - buffer) / safeScale);
  const endTime = Math.min(duration, (scrollLeft + wrapperWidth + buffer - edgeGutter) / safeScale);

  const startMajor = Math.floor(startTime / tickInterval) * tickInterval;
  const ticks: Tick[] = [];

  // 当刻度像素间隔太小时，降低标签显示频率，防止重叠
  const pixelInterval = tickInterval * safeScale;
  const labelStep = pixelInterval < 40 ? Math.ceil(40 / pixelInterval) : 1;

  let tickIndex = 0;
  for (let t = startMajor; t <= endTime + tickInterval; t += tickInterval) {
    if (t < 0 || t > duration) {
      tickIndex++;
      continue;
    }

    const time = Math.round(t * 10) / 10;
    const isMajor = tickInterval < 60 ? Math.abs(time % 60) < 0.01 : true;
    const showLabel = tickIndex % labelStep === 0;
    ticks.push({ time, isMajor, label: showLabel ? formatTime(time) : '' });

    // 插入次级刻度（一半主间隔用中高次刻度；更密的用短小刻度）
    if (subInterval > 0) {
      const useShortSub = subInterval < tickInterval / 2 - 0.001;
      let subTime = time + subInterval;
      const nextMajor = time + tickInterval;
      while (subTime < nextMajor - 0.001 && subTime <= duration) {
        ticks.push({
          time: Math.round(subTime * 10) / 10,
          isMajor: false,
          label: '',
          isSub: useShortSub,
        });
        subTime += subInterval;
      }
    }

    tickIndex++;
  }

  // 如果终点 duration 不在已有刻度中，且可见，补一个终点刻度（始终显示标签）
  const hasDurationTick = ticks.some((t) => Math.abs(t.time - duration) < 0.01);
  if (!hasDurationTick && duration > startTime && duration <= endTime + tickInterval) {
    const time = Math.round(duration * 10) / 10;
    // 如果终点和最近的有标签刻度距离太近，清空那个刻度的标签避免重叠
    const nearestLabeledIndex = ticks.findIndex(
      (t) => t.label && Math.abs(t.time - duration) * safeScale < 50
    );
    if (nearestLabeledIndex >= 0) {
      ticks[nearestLabeledIndex]!.label = '';
    }
    ticks.push({ time, isMajor: true, label: formatTime(time) });
  }

  return ticks;
}

const VideoTimeline: FC<VideoTimelineProps> = ({
  duration,
  currentTime,
  selectedRanges,
  previewRanges = [],
  maxTotalDuration,
  zoomLevel: zoomLevelProp,
  onZoomLevelChange,
  activeRangeId: activeRangeIdProp,
  onActiveRangeChange,
  onTimeChange,
  onRangeSelect,
  onRangeDelete,
  onRangeUpdate,
  onPreviewRangeClick,
  readOnly = false,
}) => {
  const [internalZoomLevel, setInternalZoomLevel] = useState(1);
  const isZoomControlled = zoomLevelProp !== undefined && onZoomLevelChange !== undefined;
  const zoomLevel = isZoomControlled ? zoomLevelProp : internalZoomLevel;

  const setZoomLevel = useCallback(
    (value: number | ((prev: number) => number)) => {
      const prev = isZoomControlled ? zoomLevelProp! : internalZoomLevel;
      const next = typeof value === 'function' ? value(prev) : value;

      if (isZoomControlled) {
        onZoomLevelChange!(next);
      } else {
        setInternalZoomLevel(next);
      }
    },
    [internalZoomLevel, isZoomControlled, onZoomLevelChange, zoomLevelProp]
  );

  const [wrapperWidth, setWrapperWidth] = useState(1200);
  const [isDragging, setIsDragging] = useState(false);
  const [dragStart, setDragStart] = useState<number | null>(null);
  const [dragEnd, setDragEnd] = useState<number | null>(null);
  const [hasDragged, setHasDragged] = useState(false);
  const [scrollOffset, setScrollOffset] = useState(0);
  const [resizingId, setResizingId] = useState<string | null>(null);
  const [internalActiveRangeId, setInternalActiveRangeId] = useState<string | null>(null);
  const isActiveControlled = activeRangeIdProp !== undefined && onActiveRangeChange !== undefined;
  const activeRangeId = isActiveControlled ? activeRangeIdProp! : internalActiveRangeId;

  const setActiveRangeId = useCallback(
    (value: string | null | ((prev: string | null) => string | null)) => {
      const prev = isActiveControlled ? activeRangeIdProp! : internalActiveRangeId;
      const next = typeof value === 'function' ? value(prev) : value;

      if (isActiveControlled) {
        onActiveRangeChange!(next);
      } else {
        setInternalActiveRangeId(next);
      }
    },
    [activeRangeIdProp, internalActiveRangeId, isActiveControlled, onActiveRangeChange]
  );

  const [resizeEdge, setResizeEdge] = useState<'start' | 'end' | null>(null);
  const [resizePreview, setResizePreview] = useState<{ id: string; start: number; end: number } | null>(null);

  // 监听 wrapper 宽度变化，动态计算 baseScale
  useEffect(() => {
    const wrapper = wrapperRef.current;
    if (!wrapper) return;

    const observer = new ResizeObserver((entries) => {
      for (const entry of entries) {
        setWrapperWidth(entry.contentRect.width);
      }
    });

    observer.observe(wrapper);
    return () => observer.disconnect();
  }, []);

  // baseScale = 1x 时每秒对应像素数，两端预留刻度文字空间
  const trackInnerWidth = Math.max(wrapperWidth - TIMELINE_EDGE_GUTTER * 2, 0);
  const baseScale =
    Number.isFinite(duration) && duration > 0 && trackInnerWidth > 0
      ? trackInnerWidth / duration
      : 50;
  const safeScale = baseScale * zoomLevel;

  const containerRef = useRef<HTMLDivElement>(null);
  const wrapperRef = useRef<HTMLDivElement>(null);
  const contentRef = useRef<HTMLDivElement>(null);
  const dragStartX = useRef(0);
  const dragStartY = useRef(0);
  const prevActiveRangeIdRef = useRef<string | null>(null);
  const scrollRafRef = useRef<number>(0);
  const programmaticScrollRef = useRef(false);
  const userScrollLockedRef = useRef(false);
  const userScrollUnlockTimerRef = useRef<ReturnType<typeof setTimeout>>();

  const markManualScroll = useCallback(() => {
    userScrollLockedRef.current = true;
    if (userScrollUnlockTimerRef.current) {
      clearTimeout(userScrollUnlockTimerRef.current);
    }
    userScrollUnlockTimerRef.current = setTimeout(() => {
      userScrollLockedRef.current = false;
    }, 3000);
  }, []);

  const applyScrollLeft = useCallback((wrapper: HTMLDivElement, targetScroll: number) => {
    programmaticScrollRef.current = true;
    wrapper.scrollLeft = targetScroll;
  }, []);

  const scrollTimeToCenter = useCallback(
    (time: number) => {
      const wrapper = wrapperRef.current;
      const content = contentRef.current;
      if (!wrapper || !content) return;
      if (wrapper.scrollWidth <= wrapper.clientWidth) return;

      userScrollLockedRef.current = false;
      if (userScrollUnlockTimerRef.current) {
        clearTimeout(userScrollUnlockTimerRef.current);
        userScrollUnlockTimerRef.current = undefined;
      }

      const viewportWidth = wrapper.clientWidth;
      const contentWidth = content.clientWidth;
      const maxScroll = Math.max(0, contentWidth - viewportWidth);
      const centerX = toTimelineX(time, safeScale);
      const targetScroll = Math.max(0, Math.min(maxScroll, centerX - viewportWidth / 2));
      applyScrollLeft(wrapper, targetScroll);
    },
    [safeScale, applyScrollLeft]
  );

  const trackWidth =
    Number.isFinite(duration) && Number.isFinite(safeScale) ? Math.max(duration * safeScale, 0) : 0;
  const timelineWidth = trackWidth + TIMELINE_EDGE_GUTTER * 2;

  const { tickInterval, subInterval } = useMemo(
    () => getTickConfig(duration, safeScale, wrapperWidth),
    [duration, safeScale, wrapperWidth]
  );

  // 监听滚动，只渲染视野内刻度；用户手动滚动时锁定，避免被播放/缩放抢滚动位置
  useEffect(() => {
    const wrapper = wrapperRef.current;
    if (!wrapper) return;

    const handleScroll = () => {
      if (!programmaticScrollRef.current) {
        markManualScroll();
      }
      programmaticScrollRef.current = false;

      if (scrollRafRef.current) return;
      scrollRafRef.current = requestAnimationFrame(() => {
        setScrollOffset(wrapper.scrollLeft);
        scrollRafRef.current = 0;
      });
    };

    const handlePointerDown = (event: PointerEvent) => {
      if (wrapper.scrollWidth <= wrapper.clientWidth) return;

      const rect = wrapper.getBoundingClientRect();
      const scrollbarZone = 16;
      if (event.clientY >= rect.bottom - scrollbarZone) {
        markManualScroll();
      }
    };

    wrapper.addEventListener('scroll', handleScroll);
    wrapper.addEventListener('pointerdown', handlePointerDown);
    return () => {
      wrapper.removeEventListener('scroll', handleScroll);
      wrapper.removeEventListener('pointerdown', handlePointerDown);
      if (scrollRafRef.current) {
        cancelAnimationFrame(scrollRafRef.current);
      }
      if (userScrollUnlockTimerRef.current) {
        clearTimeout(userScrollUnlockTimerRef.current);
      }
    };
  }, [markManualScroll]);

  const ticks: Tick[] = useMemo(() => {
    const wrapper = wrapperRef.current;
    const wrapperWidth = wrapper?.clientWidth || 0;
    return generateVisibleTicks(
      duration,
      tickInterval,
      subInterval,
      safeScale,
      scrollOffset,
      wrapperWidth,
      TIMELINE_EDGE_GUTTER
    );
  }, [duration, tickInterval, subInterval, safeScale, scrollOffset]);

  const getTimeFromX = useCallback(
    (clientX: number): number => {
      if (!contentRef.current) return 0;
      const rect = contentRef.current.getBoundingClientRect();
      const x = clientX - rect.left;
      let time = fromTimelineX(x, safeScale);
      time = Math.max(0, Math.min(time, duration));
      return Math.round(time * 100) / 100;
    },
    [safeScale, duration]
  );

  const handleMouseDown = useCallback(
    (e: React.MouseEvent) => {
      if (e.button !== 0) return;
      if ((e.target as HTMLElement).closest('.timeline-range-delete')) return;
      if ((e.target as HTMLElement).closest('.timeline-range-handle')) return;
      if ((e.target as HTMLElement).closest('.timeline-range')) return;

      const time = getTimeFromX(e.clientX);
      if (readOnly) {
        onTimeChange(time);
        return;
      }

      dragStartX.current = e.clientX;
      dragStartY.current = e.clientY;
      setIsDragging(true);
      setHasDragged(false);
      setDragStart(time);
      setDragEnd(time);
    },
    [getTimeFromX, onTimeChange, readOnly]
  );

  useEffect(() => {
    if (activeRangeId && !selectedRanges.some((range) => range.id === activeRangeId)) {
      setActiveRangeId(null);
    }
  }, [activeRangeId, selectedRanges, setActiveRangeId]);

  const handleRangeClick = useCallback(
    (range: TimeRange) => {
      setActiveRangeId(range.id);
      onTimeChange(range.start);
    },
    [onTimeChange, setActiveRangeId]
  );

  const handlePreviewRangeClick = useCallback(
    (range: TimeRange) => {
      if (onPreviewRangeClick) {
        onPreviewRangeClick(range);
        return;
      }
      onTimeChange(range.start);
    },
    [onPreviewRangeClick, onTimeChange]
  );

  const handleRangeDeleteClick = useCallback(
    (rangeId: string) => {
      if (readOnly) return;
      if (activeRangeId === rangeId) {
        setActiveRangeId(null);
      }
      onRangeDelete(rangeId);
    },
    [activeRangeId, onRangeDelete, readOnly, setActiveRangeId]
  );

  const handleResizeStart = useCallback(
    (e: React.MouseEvent, id: string, edge: 'start' | 'end') => {
      if (readOnly) return;
      e.stopPropagation();
      e.preventDefault();
      if (e.button !== 0) return;
      const range = selectedRanges.find((r) => r.id === id);
      if (!range) return;

      setActiveRangeId(id);
      setResizingId(id);
      setResizeEdge(edge);
      setResizePreview({ id, start: range.start, end: range.end });
      dragStartX.current = e.clientX;
      dragStartY.current = e.clientY;
    },
    [readOnly, selectedRanges, setActiveRangeId]
  );

  const getTotalSelectedDuration = useCallback((): number => {
    return selectedRanges.reduce((sum, r) => sum + (r.end - r.start), 0);
  }, [selectedRanges]);

  const checkMaxDuration = useCallback(
    (additionalDuration: number): boolean => {
      if (!maxTotalDuration || maxTotalDuration <= 0) return true;
      const currentTotal = getTotalSelectedDuration();
      return currentTotal + additionalDuration <= maxTotalDuration;
    },
    [maxTotalDuration, getTotalSelectedDuration]
  );

  const handleMouseUp = useCallback(() => {
    // 处理 resize 结束
    if (resizingId && resizePreview && resizeEdge && onRangeUpdate) {
      const originalRange = selectedRanges.find((r) => r.id === resizingId);
      const originalDuration = originalRange ? originalRange.end - originalRange.start : 0;
      const newDuration = resizePreview.end - resizePreview.start;
      const additionalDuration = newDuration - originalDuration;

      if (!checkMaxDuration(additionalDuration)) {
        toast.notify.warning(`选中总时长不能超过 ${formatTime(maxTotalDuration || 0)}`);
      } else if (resizePreview.end - resizePreview.start >= MIN_RANGE_DURATION) {
        onRangeUpdate({
          id: resizingId,
          start: Math.round(resizePreview.start * 100) / 100,
          end: Math.round(resizePreview.end * 100) / 100,
        });
        onTimeChange(resizePreview.start);
      } else {
        toast.notify.warning(`片段时长不能少于 ${MIN_RANGE_DURATION} 秒`);
      }

      setResizingId(null);
      setResizeEdge(null);
      setResizePreview(null);
      return;
    }

    if (!isDragging) return;

    if (dragStart !== null && dragEnd !== null) {
      const start = Math.min(dragStart, dragEnd);
      const end = Math.max(dragStart, dragEnd);
      const rangeDuration = end - start;

      if (rangeDuration >= MIN_RANGE_DURATION) {
        if (!checkMaxDuration(rangeDuration)) {
          toast.notify.warning(`选中总时长不能超过 ${formatTime(maxTotalDuration || 0)}`);
        } else {
          const newRangeId = `range-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`;
          onRangeSelect({
            id: newRangeId,
            start: Math.round(start * 100) / 100,
            end: Math.round(end * 100) / 100,
          });
          setActiveRangeId(newRangeId);
          onTimeChange(start);
        }
      } else if (hasDragged && rangeDuration > 0) {
        toast.notify.warning(`片段时长不能少于 ${MIN_RANGE_DURATION} 秒`);
      } else if (!hasDragged) {
        setActiveRangeId(null);
        onTimeChange(dragStart);
      }
    }

    setIsDragging(false);
    setDragStart(null);
    setDragEnd(null);
    setHasDragged(false);
  }, [
    isDragging,
    hasDragged,
    dragStart,
    dragEnd,
    checkMaxDuration,
    maxTotalDuration,
    onRangeSelect,
    onTimeChange,
    resizingId,
    resizeEdge,
    resizePreview,
    onRangeUpdate,
    selectedRanges,
    setActiveRangeId,
  ]);

  useEffect(() => {
    // resize 模式下的全局鼠标移动
    if (resizingId && resizeEdge && resizePreview) {
      const handleGlobalResizeMove = (e: MouseEvent) => {
        const time = getTimeFromX(e.clientX);
        setResizePreview((prev) => {
          if (!prev) return null;
          if (resizeEdge === 'start') {
            return {
              ...prev,
              start: Math.max(0, Math.min(time, prev.end - MIN_RANGE_DURATION)),
            };
          }
          return {
            ...prev,
            end: Math.min(duration, Math.max(time, prev.start + MIN_RANGE_DURATION)),
          };
        });
      };

      const handleGlobalResizeUp = () => {
        handleMouseUp();
      };

      document.addEventListener('mousemove', handleGlobalResizeMove);
      document.addEventListener('mouseup', handleGlobalResizeUp);

      return () => {
        document.removeEventListener('mousemove', handleGlobalResizeMove);
        document.removeEventListener('mouseup', handleGlobalResizeUp);
      };
    }

    if (!isDragging) return;

    const handleGlobalMouseMove = (e: MouseEvent) => {
      const dist = Math.sqrt(
        Math.pow(e.clientX - dragStartX.current, 2) + Math.pow(e.clientY - dragStartY.current, 2)
      );
      const time = getTimeFromX(e.clientX);
      if (dist > 3) {
        setHasDragged(true);
      } else if (dragStart !== null && Math.abs(time - dragStart) >= MIN_RANGE_DURATION * 0.2) {
        setHasDragged(true);
      }
      setDragEnd(time);
    };

    const handleGlobalMouseUp = () => {
      handleMouseUp();
    };

    document.addEventListener('mousemove', handleGlobalMouseMove);
    document.addEventListener('mouseup', handleGlobalMouseUp);

    return () => {
      document.removeEventListener('mousemove', handleGlobalMouseMove);
      document.removeEventListener('mouseup', handleGlobalMouseUp);
    };
  }, [isDragging, dragStart, duration, getTimeFromX, handleMouseUp, resizingId, resizeEdge, resizePreview]);

  // 缩放变化时将播放头滚入视野；播放过程中不自动抢滚动位置
  useEffect(() => {
    if (!wrapperRef.current || !contentRef.current) return;
    if (userScrollLockedRef.current) return;

    const wrapper = wrapperRef.current;
    const playheadLeft = toTimelineX(currentTime, safeScale);
    const viewportWidth = wrapper.clientWidth;
    const contentWidth = contentRef.current.clientWidth;

    const maxScroll = Math.max(0, contentWidth - viewportWidth);
    let targetScroll = wrapper.scrollLeft;

    if (playheadLeft < targetScroll || playheadLeft > targetScroll + viewportWidth) {
      targetScroll = Math.max(0, Math.min(maxScroll, playheadLeft - viewportWidth / 2));
    }

    if (contentWidth <= viewportWidth) {
      targetScroll = 0;
    }

    applyScrollLeft(wrapper, targetScroll);
  }, [safeScale, applyScrollLeft, duration]);

  // 切换选中片段时，将片段滚到时间轴中央（如点击上方 tab）
  useEffect(() => {
    if (!activeRangeId) {
      prevActiveRangeIdRef.current = null;
      return;
    }
    if (activeRangeId === prevActiveRangeIdRef.current) return;
    if (isDragging || resizingId) return;

    const range = selectedRanges.find((item) => item.id === activeRangeId);
    if (!range) return;

    prevActiveRangeIdRef.current = activeRangeId;
    scrollTimeToCenter((range.start + range.end) / 2);
  }, [activeRangeId, isDragging, resizingId, scrollTimeToCenter, selectedRanges]);

  // Ctrl+滚轮切换 zoomLevel（1x~6x）
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;

    const handleWheel = (e: WheelEvent) => {
      if (e.ctrlKey || e.metaKey) {
        e.preventDefault();
        if (e.deltaY > 0) {
          setZoomLevel((prev) => Math.max(1, prev - 1));
        } else {
          setZoomLevel((prev) => Math.min(10, prev + 1));
        }
      }
    };

    el.addEventListener('wheel', handleWheel, { passive: false });
    return () => {
      el.removeEventListener('wheel', handleWheel);
    };
  }, []);

  const selectionStart = dragStart !== null && dragEnd !== null ? Math.min(dragStart, dragEnd) : null;
  const selectionEnd = dragStart !== null && dragEnd !== null ? Math.max(dragStart, dragEnd) : null;
  const isInteracting = Boolean(resizingId) || isDragging;

  const rangeIndexMap = useMemo(() => {
    return new Map(selectedRanges.map((range, index) => [range.id, index + 1]));
  }, [selectedRanges]);

  useEffect(() => {
    if (!isInteracting) return;

    const previousCursor = document.body.style.cursor;
    const previousUserSelect = document.body.style.userSelect;
    document.body.style.cursor = 'col-resize';
    document.body.style.userSelect = 'none';

    return () => {
      document.body.style.cursor = previousCursor;
      document.body.style.userSelect = previousUserSelect;
    };
  }, [isInteracting]);

  return (
    <div
      className={`video-timeline${zoomLevel > 1 ? ' has-zoom-scroll' : ''}${resizingId ? ' is-resizing' : ''}${isDragging ? ' is-selecting' : ''}`}
      ref={containerRef}
      style={{ '--timeline-edge-gutter': `${TIMELINE_EDGE_GUTTER}px` } as CSSProperties}
    >
      <div className={`timeline-wrapper ${zoomLevel > 1 ? 'has-scroll' : ''}`} ref={wrapperRef}>
        <div
          className="timeline-content"
          ref={contentRef}
          style={{ width: timelineWidth }}
          onMouseDown={handleMouseDown}
        >
          <div className="timeline-ruler">
            {ticks.map((tick) => {
              const tickLeft = toTimelineX(tick.time, safeScale);
              const isStart = tick.time <= 0.01;
              const isEnd = Math.abs(tick.time - duration) < 0.01;
              const viewportLeft = scrollOffset;
              const viewportRight = scrollOffset + wrapperWidth;
              const isNearViewportStart =
                Boolean(tick.label) && tickLeft - viewportLeft < TIMELINE_LABEL_EDGE_THRESHOLD;
              const isNearViewportEnd =
                Boolean(tick.label) && viewportRight - tickLeft < TIMELINE_LABEL_EDGE_THRESHOLD;

              return (
                <div
                  key={tick.time}
                  className={[
                    'timeline-tick',
                    tick.isMajor ? 'major' : '',
                    tick.isSub ? 'sub' : '',
                    isStart || isNearViewportStart ? 'align-start' : '',
                    isEnd || isNearViewportEnd ? 'align-end' : '',
                  ]
                    .filter(Boolean)
                    .join(' ')}
                  style={{ left: tickLeft }}
                >
                  {!tick.isSub && <span className="timeline-tick-label">{tick.label}</span>}
                  <div className="timeline-tick-line" />
                </div>
              );
            })}
          </div>

          <div className="timeline-track" />

          <div className="timeline-overlays">
            {previewRanges.map((range) => {
              const width = Math.max((range.end - range.start) * safeScale, 4);
              const rangeLabel = range.title?.trim() || 'AI选片';
              const timeLabel = `${formatTime(range.start)} - ${formatTime(range.end)}`;
              return (
                <div
                  key={range.id}
                  className="timeline-range timeline-range_preview"
                  style={{
                    left: toTimelineX(range.start, safeScale),
                    width,
                  }}
                  title={`${rangeLabel}\n${timeLabel}\n点击加入已选片段`}
                  onClick={() => handlePreviewRangeClick(range)}
                >
                  <div className="timeline-range-body">
                    <span className="timeline-range-label" aria-hidden="true">
                      {rangeLabel}
                    </span>
                  </div>
                </div>
              );
            })}

            {selectedRanges.map((range) => {
              const isResizing = resizingId === range.id;
              const preview = isResizing && resizePreview ? resizePreview : null;
              const visual = buildRangeVisual(range, preview);
              const { containerStart, containerEnd, previewStart, previewEnd, segments } = visual;
              const containerWidth = Math.max((containerEnd - containerStart) * safeScale, 4);
              const previewOffset = (previewStart - containerStart) * safeScale;
              const previewWidth = Math.max((previewEnd - previewStart) * safeScale, 4);

              const isActive = activeRangeId === range.id;
              const isNarrowRange = previewWidth < NARROW_RANGE_DELETE_WIDTH;
              const rangeLabel = `片段${rangeIndexMap.get(range.id) ?? ''}`;

              return (
                <div
                  key={range.id}
                  className={`timeline-range ${isResizing ? 'resizing' : ''}${isActive ? ' active' : ''}${isNarrowRange ? ' narrow' : ''}`}
                  style={{
                    left: toTimelineX(containerStart, safeScale),
                    width: containerWidth,
                  }}
                  title={`${rangeLabel}\n${formatTime(previewStart)} - ${formatTime(previewEnd)}\n点击从该区域开始播放`}
                  onClick={() => handleRangeClick(range)}
                >
                  <div className="timeline-range-segments" aria-hidden="true">
                    {segments.map((segment) => (
                      <div
                        key={`${segment.type}-${segment.start}-${segment.end}`}
                        className={`timeline-range-segment timeline-range-segment_${segment.type}`}
                        style={{
                          left: (segment.start - containerStart) * safeScale,
                          width: Math.max((segment.end - segment.start) * safeScale, 1),
                        }}
                      />
                    ))}
                  </div>

                  <div
                    className="timeline-range-body"
                    style={{
                      left: previewOffset,
                      width: previewWidth,
                    }}
                  >
                    {!readOnly ? (
                      <div
                        className="timeline-range-handle left"
                        onMouseDown={(e) => handleResizeStart(e, range.id, 'start')}
                        title="拖动调整起始时间"
                      />
                    ) : null}
                    <span className="timeline-range-label" aria-hidden="true">
                      {rangeLabel}
                    </span>
                    {!readOnly ? (
                      <>
                        <div
                          className="timeline-range-handle right"
                          onMouseDown={(e) => handleResizeStart(e, range.id, 'end')}
                          title="拖动调整结束时间"
                        />
                        <button
                          className="timeline-range-delete"
                          onClick={(e) => {
                            e.stopPropagation();
                            handleRangeDeleteClick(range.id);
                          }}
                          title="删除该时间段"
                        >
                          ×
                        </button>
                      </>
                    ) : null}
                  </div>
                </div>
              );
            })}

            {selectionStart !== null && selectionEnd !== null && (hasDragged || Math.abs(selectionEnd - selectionStart) >= MIN_RANGE_DURATION * 0.2) && (
              <div
                className="timeline-selection timeline-selection_added"
                style={{
                  left: toTimelineX(selectionStart, safeScale),
                  width: Math.max((selectionEnd - selectionStart) * safeScale, 4),
                }}
              />
            )}
          </div>

          <div className="timeline-playhead" style={{ left: toTimelineX(currentTime, safeScale) }}>
            <div className="timeline-playhead-triangle" />
            <div className="timeline-playhead-line" />
            <div className="timeline-playhead-time">{formatTimeMs(currentTime)}</div>
          </div>
        </div>
      </div>
    </div>
  );
};

export default VideoTimeline;
