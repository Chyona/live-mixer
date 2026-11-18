import { forwardRef, useCallback, useEffect, useImperativeHandle, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Checkbox } from 'antd';
import {
  LuArrowLeft,
  LuCornerDownRight,
  LuGripVertical,
  LuMinus,
  LuPlay,
  LuPlus,
  LuScissors,
  LuTextSelect,
  LuTrash2,
} from 'react-icons/lu';
import { CopyIcon } from '~/components/CopyIcon';
import type {
  AiSegment,
  CopyInsertMode,
  SelectedCopySegment,
  TranscriptParagraph,
} from '../types';
import {
  formatPadSeconds,
  formatSliceTime,
  getSegmentAdjustableSeconds,
  getSegmentBackPadSeconds,
  getSegmentFrontPadSeconds,
  getSpeakerColor,
  getTextSelectionOffsets,
  getTotalSelectedDuration,
  moveSegmentsByIds,
  scrollElementIntoViewPreferUpper,
  SEGMENT_EXTEND_STEP_SEC,
} from '../utils';
import { formatVideoDuration } from '~/utils/duration';
import VideoTranscriptResizeHandle from './VideoTranscriptResizeHandle';

type DropMarker = {
  index: number;
  placement: 'before' | 'after';
};

type DragGhost = {
  index: number;
  /** 本次拖动的片段数量（多选拖动时 > 1） */
  count: number;
  x: number;
  y: number;
  width: number;
  offsetX: number;
  offsetY: number;
};

function measureAiBlockContentHeight(block: HTMLElement): number {
  const head = block.querySelector<HTMLElement>('.slice-editor-ai-block-head');
  const list = block.querySelector<HTMLElement>('.slice-editor-ai-block-list');
  const style = getComputedStyle(block);
  const paddingTop = Number.parseFloat(style.paddingTop) || 0;
  const paddingBottom = Number.parseFloat(style.paddingBottom) || 0;
  const headHeight = head?.offsetHeight ?? 0;
  const headMarginBottom = head ? Number.parseFloat(getComputedStyle(head).marginBottom) || 0 : 0;
  const listHeight = list?.scrollHeight ?? 0;
  return Math.ceil(paddingTop + paddingBottom + headHeight + headMarginBottom + listHeight);
}

const AI_BLOCK_MIN_HEIGHT = 72;
const AI_BLOCK_MAX_HEIGHT_RATIO = 0.65;
/** 拖拽靠近列表上下缘时触发自动滚动的区域高度（px） */
const DRAG_AUTO_SCROLL_EDGE_PX = 48;
/** 拖拽自动滚动最大速度（px / frame） */
const DRAG_AUTO_SCROLL_MAX_SPEED_PX = 18;

function getDropInsertIndex(target: DropMarker, length: number) {
  return target.placement === 'before'
    ? target.index
    : Math.min(target.index + 1, length);
}

/** 多选拖动落点是否会产生顺序变化（落在选中块内部时视为无变化）。 */
function wouldReorderSelection(
  segments: SelectedCopySegment[],
  ids: string[],
  target: DropMarker
) {
  if (!segments.length || !ids.length) return false;
  return (
    moveSegmentsByIds(segments, ids, getDropInsertIndex(target, segments.length)) !== segments
  );
}

export type SelectedCopyPanelHandle = {
  /** 滚动到指定片段；flash 为 true 时短暂高亮，用于提示新加入位置 */
  scrollToSegment: (id: string, options?: { flash?: boolean }) => void;
  /** 「跟随选中」模式下新增文案的插入锚点（选中块最后一段）；无选中时返回 null */
  getInsertAnchorId: () => string | null;
};

interface SelectedCopyPanelProps {
  segments: SelectedCopySegment[];
  /** 文案分段原始数据，用于前后留白边界 */
  paragraphs: TranscriptParagraph[];
  /** AI 分段（asr_summaries），展示在文案预览上方 */
  aiSegments?: AiSegment[];
  currentTime?: number;
  activeSegmentId: string | null;
  /** 左侧加入文案时的插入方式；after-selected 时在锚点片段上展示提示 */
  insertMode?: CopyInsertMode;
  speakers: string[];
  maxTotalDuration: number;
  videoDuration: number;
  submitting: boolean;
  enableCaptions: boolean;
  onEnableCaptionsChange: (checked: boolean) => void;
  onActiveSegmentChange: (segmentId: string | null) => void;
  onSeek: (time: number) => void;
  onReorder: (segments: SelectedCopySegment[]) => void;
  onDeleteSegment: (segmentId: string) => void;
  onDeleteSelectedRange: (
    segmentId: string,
    textElement: HTMLElement | null,
    savedSelection?: { start: number; end: number } | null
  ) => void;
  onCopySegment: (segmentId: string) => void;
  onAdjustSegment: (segmentId: string, edge: 'start' | 'end', deltaSec: number) => void;
  onAddAiSegment?: (segment: AiSegment) => void;
  onClearAll: () => void;
  onPreview: () => void;
  onSave: () => void;
  savingProject?: boolean;
  onSaveAs: () => void;
  onExportDraft: () => void;
  onSubmit: () => void;
  /** 只读：可预览定位，不可保存/提交/增删改 */
  readOnly?: boolean;
}

const SelectedCopyPanel = forwardRef<SelectedCopyPanelHandle, SelectedCopyPanelProps>(function SelectedCopyPanel({
  segments,
  paragraphs,
  aiSegments = [],
  currentTime = 0,
  activeSegmentId,
  insertMode = 'timeline',
  speakers,
  maxTotalDuration,
  videoDuration,
  submitting,
  enableCaptions,
  onEnableCaptionsChange,
  onActiveSegmentChange,
  onSeek,
  onReorder,
  onDeleteSegment,
  onDeleteSelectedRange,
  onCopySegment,
  onAdjustSegment,
  onAddAiSegment,
  onClearAll,
  onPreview,
  onSave,
  savingProject = false,
  onSaveAs,
  onExportDraft,
  onSubmit,
  readOnly = false,
}, ref) {
  const [dragIds, setDragIds] = useState<string[]>([]);
  const [dropMarker, setDropMarker] = useState<DropMarker | null>(null);
  const [dragGhost, setDragGhost] = useState<DragGhost | null>(null);
  /** 刚从左侧加入的片段：短暂高亮，帮助用户定位 */
  const [flashSegmentId, setFlashSegmentId] = useState<string | null>(null);
  /** 多选：与系统文件管理器一致，Ctrl/Cmd 点选切换、Shift 点选连续范围 */
  const [selectedIds, setSelectedIds] = useState<string[]>([]);
  const [aiPanelHeight, setAiPanelHeight] = useState<number | null>(null);
  const dragIdsRef = useRef<string[]>([]);
  const selectedIdsRef = useRef<string[]>([]);
  const selectionAnchorIdRef = useRef<string | null>(null);
  /** 记录由本组件主动设置的 active id，避免外部 active 同步逻辑覆盖多选 */
  const selfActiveIdRef = useRef<string | null>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const aiBlockRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const flashTimerRef = useRef(0);

  /** 「跟随选中」的插入锚点：多选时取列表中最后一段，未多选时取 active 片段。 */
  const insertAnchorId = useMemo(() => {
    if (insertMode !== 'after-selected') return null;
    const selectedInOrder = segments.filter((segment) => selectedIds.includes(segment.id));
    const lastSelected = selectedInOrder[selectedInOrder.length - 1];
    if (lastSelected) return lastSelected.id;
    if (activeSegmentId && segments.some((segment) => segment.id === activeSegmentId)) {
      return activeSegmentId;
    }
    return null;
  }, [activeSegmentId, insertMode, segments, selectedIds]);

  const revealSegment = (id: string, attempt = 0) => {
    const container = listRef.current;
    if (!container) return;
    const node = container.querySelector<HTMLElement>(`[data-copy-id="${CSS.escape(id)}"]`);
    // 刚 setState 后节点可能尚未挂载：重试几帧再滚动。
    if (!node) {
      if (attempt < 6) {
        requestAnimationFrame(() => revealSegment(id, attempt + 1));
      }
      return;
    }
    scrollElementIntoViewPreferUpper(container, node);
    setFlashSegmentId(id);
    window.clearTimeout(flashTimerRef.current);
    flashTimerRef.current = window.setTimeout(() => setFlashSegmentId(null), 1800);
  };

  useImperativeHandle(ref, () => ({
    scrollToSegment(id: string, options?: { flash?: boolean }) {
      if (options?.flash) {
        revealSegment(id);
        return;
      }
      const container = listRef.current;
      if (!container) return;
      const node = container.querySelector<HTMLElement>(`[data-copy-id="${CSS.escape(id)}"]`);
      if (!node) return;
      scrollElementIntoViewPreferUpper(container, node);
    },
    getInsertAnchorId() {
      return insertAnchorId;
    },
  }));
  const pointerDraggingRef = useRef(false);
  const suppressItemClickRef = useRef(false);
  const textSelectionRef = useRef<{ segmentId: string; start: number; end: number } | null>(null);
  const dragPointerYRef = useRef(0);
  const dragAutoScrollRafRef = useRef(0);
  const segmentsRef = useRef(segments);
  segmentsRef.current = segments;
  const totalDuration = getTotalSelectedDuration(segments);
  const isOverLimit = totalDuration > maxTotalDuration;
  const canDragSort = !readOnly && segments.length > 1;
  const hasSegments = segments.length > 0;
  const selectedIdSet = useMemo(() => new Set(selectedIds), [selectedIds]);
  const dragIdSet = useMemo(() => new Set(dragIds), [dragIds]);
  const showMultiSelection = selectedIds.length > 1;
  const showAiResizeHandle = aiSegments.length > 0;
  const activeAiSegmentId = useMemo(() => {
    if (!aiSegments.length) return null;
    return (
      aiSegments.find((item) => currentTime >= item.start && currentTime < item.end)?.id ?? null
    );
  }, [aiSegments, currentTime]);
  const draggingSegment = dragIds.length
    ? segments.find((segment) => segment.id === dragIds[0]) ?? null
    : null;
  const showDropMarker =
    dragIds.length > 0 &&
    dropMarker != null &&
    wouldReorderSelection(segments, dragIds, dropMarker);

  useEffect(() => {
    if (!aiSegments.length) {
      setAiPanelHeight(null);
    }
  }, [aiSegments]);

  // 外部（左侧文案分段、加载项目等）切换 active 时，把多选收敛为该片段。
  useEffect(() => {
    if (!activeSegmentId) {
      selectedIdsRef.current = [];
      setSelectedIds([]);
      selectionAnchorIdRef.current = null;
      selfActiveIdRef.current = null;
      return;
    }
    if (selfActiveIdRef.current === activeSegmentId) {
      selfActiveIdRef.current = null;
      return;
    }
    selectedIdsRef.current = [activeSegmentId];
    setSelectedIds([activeSegmentId]);
    selectionAnchorIdRef.current = activeSegmentId;
  }, [activeSegmentId]);

  // 片段被删除/替换后清理已失效的选中项。
  useEffect(() => {
    setSelectedIds((prev) => {
      const existing = new Set(segments.map((segment) => segment.id));
      const next = prev.filter((id) => existing.has(id));
      if (next.length === prev.length) return prev;
      selectedIdsRef.current = next;
      return next;
    });
  }, [segments]);

  useEffect(() => {
    return () => {
      if (dragAutoScrollRafRef.current) {
        cancelAnimationFrame(dragAutoScrollRafRef.current);
      }
      window.clearTimeout(flashTimerRef.current);
    };
  }, []);

  const stopDragAutoScroll = useCallback(() => {
    if (dragAutoScrollRafRef.current) {
      cancelAnimationFrame(dragAutoScrollRafRef.current);
      dragAutoScrollRafRef.current = 0;
    }
  }, []);

  const getDropTarget = useCallback((clientY: number): DropMarker | null => {
    const items = listRef.current?.querySelectorAll<HTMLElement>('.slice-editor-copy-item');
    if (!items?.length) return null;

    const itemElements = Array.from(items);
    const lastIndex = itemElements.length - 1;
    for (let i = 0; i < itemElements.length; i += 1) {
      const item = itemElements[i];
      if (!item) continue;
      const rect = item.getBoundingClientRect();
      if (clientY < rect.top + rect.height / 2) {
        return { index: i, placement: 'before' };
      }
    }

    return { index: lastIndex, placement: 'after' };
  }, []);

  const updateDropMarkerAtPointer = useCallback(
    (clientY: number) => {
      const target = getDropTarget(clientY);
      if (!target) return;

      setDropMarker(target);
      if (wouldReorderSelection(segmentsRef.current, dragIdsRef.current, target)) {
        suppressItemClickRef.current = true;
      }
    },
    [getDropTarget]
  );

  const tickDragAutoScroll = useCallback(() => {
    const container = listRef.current;
    if (!container || !pointerDraggingRef.current) {
      stopDragAutoScroll();
      return;
    }

    const rect = container.getBoundingClientRect();
    const pointerY = dragPointerYRef.current;
    let delta = 0;

    if (pointerY < rect.top + DRAG_AUTO_SCROLL_EDGE_PX) {
      const distance = Math.max(0, pointerY - rect.top);
      const intensity = 1 - distance / DRAG_AUTO_SCROLL_EDGE_PX;
      delta = -Math.max(4, DRAG_AUTO_SCROLL_MAX_SPEED_PX * intensity);
    } else if (pointerY > rect.bottom - DRAG_AUTO_SCROLL_EDGE_PX) {
      const distance = Math.max(0, rect.bottom - pointerY);
      const intensity = 1 - distance / DRAG_AUTO_SCROLL_EDGE_PX;
      delta = Math.max(4, DRAG_AUTO_SCROLL_MAX_SPEED_PX * intensity);
    }

    if (delta === 0) {
      stopDragAutoScroll();
      return;
    }

    const maxScrollTop = container.scrollHeight - container.clientHeight;
    const nextScrollTop = Math.max(0, Math.min(container.scrollTop + delta, maxScrollTop));
    if (nextScrollTop !== container.scrollTop) {
      container.scrollTop = nextScrollTop;
      updateDropMarkerAtPointer(pointerY);
    }

    dragAutoScrollRafRef.current = requestAnimationFrame(tickDragAutoScroll);
  }, [stopDragAutoScroll, updateDropMarkerAtPointer]);

  const syncDragAutoScroll = useCallback(() => {
    stopDragAutoScroll();
    dragAutoScrollRafRef.current = requestAnimationFrame(tickDragAutoScroll);
  }, [stopDragAutoScroll, tickDragAutoScroll]);

  const resetDragState = () => {
    stopDragAutoScroll();
    pointerDraggingRef.current = false;
    dragIdsRef.current = [];
    setDragIds([]);
    setDropMarker(null);
    setDragGhost(null);
  };

  const finishPointerDrag = useCallback(
    (clientY: number) => {
      const list = segmentsRef.current;
      const ids = dragIdsRef.current;
      const target = getDropTarget(clientY);
      if (ids.length && target) {
        const next = moveSegmentsByIds(list, ids, getDropInsertIndex(target, list.length));
        if (next !== list) {
          onReorder(next);
          suppressItemClickRef.current = true;
        }
      }
      resetDragState();
    },
    [getDropTarget, onReorder]
  );

  const handleDragHandlePointerDown = (index: number) => (event: React.PointerEvent<HTMLButtonElement>) => {
    if (!canDragSort) return;

    event.preventDefault();
    event.stopPropagation();
    suppressItemClickRef.current = false;

    const list = segmentsRef.current;
    const segment = list[index];
    if (!segment) return;

    // 拖动已选中的片段时整组移动；拖动未选中片段时先把它设为唯一选中项。
    const currentSelection = selectedIdsRef.current;
    const movingIds =
      currentSelection.includes(segment.id) && currentSelection.length > 1
        ? list.filter((item) => currentSelection.includes(item.id)).map((item) => item.id)
        : [segment.id];

    if (movingIds.length === 1) {
      selectedIdsRef.current = movingIds;
      setSelectedIds(movingIds);
      selectionAnchorIdRef.current = segment.id;
      selfActiveIdRef.current = segment.id;
      onActiveSegmentChange(segment.id);
    }

    pointerDraggingRef.current = true;
    dragIdsRef.current = movingIds;
    setDragIds(movingIds);
    setDropMarker({ index, placement: 'before' });

    const item = event.currentTarget.closest<HTMLElement>('.slice-editor-copy-item');
    const rect = item?.getBoundingClientRect();
    if (rect) {
      setDragGhost({
        index,
        count: movingIds.length,
        x: event.clientX,
        y: event.clientY,
        width: Math.min(rect.width, 360),
        offsetX: Math.min(Math.max(event.clientX - rect.left, 12), 48),
        offsetY: Math.min(Math.max(event.clientY - rect.top, 12), 28),
      });
    }

    event.currentTarget.setPointerCapture(event.pointerId);
  };

  const handleDragHandlePointerMove = (event: React.PointerEvent<HTMLButtonElement>) => {
    if (!pointerDraggingRef.current || dragIdsRef.current.length === 0) return;

    dragPointerYRef.current = event.clientY;

    setDragGhost((prev) =>
      prev ? { ...prev, x: event.clientX, y: event.clientY } : prev
    );

    updateDropMarkerAtPointer(event.clientY);
    syncDragAutoScroll();
  };

  const handleDragHandlePointerUp = (event: React.PointerEvent<HTMLButtonElement>) => {
    if (!pointerDraggingRef.current) return;

    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }

    finishPointerDrag(event.clientY);
  };

  const handleDragHandlePointerCancel = (event: React.PointerEvent<HTMLButtonElement>) => {
    if (!pointerDraggingRef.current) return;

    if (event.currentTarget.hasPointerCapture(event.pointerId)) {
      event.currentTarget.releasePointerCapture(event.pointerId);
    }

    resetDragState();
  };

  /**
   * 片段点选：与系统文件管理器一致。
   * - 普通点击：仅选中当前片段
   * - Ctrl / Cmd + 点击：切换当前片段是否选中
   * - Shift + 点击：选中锚点到当前片段的连续范围
   */
  const handleSegmentSelect = (index: number, event: React.MouseEvent) => {
    if (suppressItemClickRef.current) {
      suppressItemClickRef.current = false;
      return;
    }
    if (pointerDraggingRef.current) return;

    const list = segmentsRef.current;
    const segment = list[index];
    if (!segment) return;

    const applySelection = (ids: string[]) => {
      selectedIdsRef.current = ids;
      setSelectedIds(ids);
      selfActiveIdRef.current = segment.id;
      onActiveSegmentChange(segment.id);
      onSeek(segment.start);
    };

    if (event.shiftKey) {
      const anchorId = selectionAnchorIdRef.current ?? activeSegmentId ?? segment.id;
      const anchorIndex = list.findIndex((item) => item.id === anchorId);
      if (anchorIndex < 0) {
        selectionAnchorIdRef.current = segment.id;
        applySelection([segment.id]);
        return;
      }
      const from = Math.min(anchorIndex, index);
      const to = Math.max(anchorIndex, index);
      applySelection(list.slice(from, to + 1).map((item) => item.id));
      return;
    }

    if (event.metaKey || event.ctrlKey) {
      const nextSet = new Set(selectedIdsRef.current);
      if (nextSet.has(segment.id)) {
        nextSet.delete(segment.id);
      } else {
        nextSet.add(segment.id);
      }
      selectionAnchorIdRef.current = segment.id;
      applySelection(list.filter((item) => nextSet.has(item.id)).map((item) => item.id));
      return;
    }

    selectionAnchorIdRef.current = segment.id;
    applySelection([segment.id]);
  };

  return (
    <div ref={panelRef} className="slice-editor-panel slice-editor-panel_copy">
      {aiSegments.length > 0 ? (
        <>
          <div
            ref={aiBlockRef}
            className={[
              'slice-editor-ai-block',
              aiPanelHeight != null ? 'slice-editor-ai-block_customized' : '',
            ]
              .filter(Boolean)
              .join(' ')}
            style={
              aiPanelHeight != null
                ? { height: aiPanelHeight, flex: `0 0 ${aiPanelHeight}px`, maxHeight: 'none' }
                : undefined
            }
          >
            <div className="slice-editor-ai-block-head">
              <div className="slice-editor-panel-title">AI分段</div>
              <span className="slice-editor-copy-stats">共 {aiSegments.length} 段</span>
            </div>
            <div className="slice-editor-ai-block-list">
              {aiSegments.map((aiSegment, index) => {
                const label = aiSegment.title?.trim() || `片段 ${index + 1}`;
                const timeLabel = `${formatVideoDuration(aiSegment.start)} - ${formatVideoDuration(aiSegment.end)}`;
                const isActive = activeAiSegmentId === aiSegment.id;
                return (
                  <div
                    key={aiSegment.id}
                    className={`slice-editor-ai-item${isActive ? ' is-active' : ''}`}
                  >
                    <button
                      type="button"
                      className="slice-editor-ai-item-main"
                      onClick={() => onSeek(aiSegment.start)}
                      title={`${label}: ${timeLabel}`}
                    >
                      <span className="slice-editor-ai-item-label">
                        <span className="slice-editor-ai-item-title">{label}</span>
                        <span className="slice-editor-ai-item-sep" aria-hidden>：</span>
                        <span className="slice-editor-ai-item-time">{timeLabel}</span>
                      </span>
                    </button>
                    {onAddAiSegment && !readOnly ? (
                      <button
                        type="button"
                        className="slice-editor-ai-item-add"
                        onClick={() => onAddAiSegment(aiSegment)}
                        title="整段加入文案预览"
                        aria-label={`将「${label}」加入文案预览`}
                      >
                        <LuPlus size={14} />
                      </button>
                    ) : null}
                  </div>
                );
              })}
            </div>
          </div>

          {showAiResizeHandle ? (
            <VideoTranscriptResizeHandle
              isCustomized={aiPanelHeight != null}
              onResize={setAiPanelHeight}
              onMeasureStart={() => aiBlockRef.current?.getBoundingClientRect().height ?? 0}
              onMeasurePanel={() => panelRef.current?.getBoundingClientRect().height ?? 0}
              onMeasureContentMax={() =>
                aiBlockRef.current
                  ? measureAiBlockContentHeight(aiBlockRef.current)
                  : Number.POSITIVE_INFINITY
              }
              onReset={() => setAiPanelHeight(null)}
              minHeight={AI_BLOCK_MIN_HEIGHT}
              maxHeightRatio={AI_BLOCK_MAX_HEIGHT_RATIO}
              ariaLabel="拖动调整 AI 分段区域高度，双击还原"
            />
          ) : null}
        </>
      ) : null}

      <div className="slice-editor-copy-top">
        <div className="slice-editor-copy-head">
          <div className="slice-editor-panel-title">文案预览</div>
          <span
            className={[
              'slice-editor-copy-stats',
              isOverLimit ? 'slice-editor-copy-stats_over' : '',
            ]
              .filter(Boolean)
              .join(' ')}
          >
            已选 {segments.length} 段 · 总时长 {formatVideoDuration(Math.round(totalDuration))}
            {isOverLimit ? ` · 超出 ${maxTotalDuration / 60} 分钟限制` : ''}
          </span>
        </div>

        <div className="slice-editor-copy-toolbar">
          <div className="slice-editor-copy-toolbar-group">
            <button type="button" onClick={onPreview} disabled={!hasSegments}>
              <LuPlay size={14} />
              连续预览
            </button>
            <button
              type="button"
              onClick={onSave}
              disabled={readOnly || !hasSegments || savingProject}
            >
              {savingProject ? '保存中...' : '保存'}
            </button>
            <button type="button" onClick={onSaveAs} disabled={readOnly || !hasSegments}>
              另存为
            </button>
          </div>
          <div className="slice-editor-copy-toolbar-group slice-editor-copy-toolbar-group_primary">
            <Checkbox
              className="slice-enable-captions-checkbox"
              checked={enableCaptions}
              disabled={readOnly}
              onChange={(event) => onEnableCaptionsChange(event.target.checked)}
            >
              生成字幕
            </Checkbox>
            <button
              type="button"
              className="danger"
              onClick={onClearAll}
              disabled={readOnly || !hasSegments}
            >
              清空
            </button>
            <button
              type="button"
              className="primary"
              onClick={onSubmit}
              disabled={readOnly || !hasSegments || submitting || isOverLimit}
            >
              {submitting ? '提交中...' : '提交成片'}
            </button>
          </div>
        </div>
      </div>

      <div
        ref={listRef}
        className={[
          'slice-editor-copy-list',
          segments.length === 0 ? 'slice-editor-copy-list_empty' : '',
          dragIds.length > 0 ? 'slice-editor-copy-list_dragging' : '',
        ]
          .filter(Boolean)
          .join(' ')}
      >
        {segments.length === 0 ? (
          <div className="slice-editor-copy-empty">
            <div className="slice-editor-copy-empty-icon" aria-hidden>
              <LuTextSelect size={22} />
            </div>
            <p className="slice-editor-copy-empty-desc">
              从左侧「文案分段」中选择整段或部分文字，内容将显示在这里
            </p>
            <span className="slice-editor-copy-empty-hint">
              <LuArrowLeft size={14} />
              双击选整段，拖选提取片段
            </span>
          </div>
        ) : (
          segments.map((segment, index) => {
            const color = getSpeakerColor(segment.speaker, speakers);
            const isActive = activeSegmentId === segment.id;
            const isMultiSelected = showMultiSelection && selectedIdSet.has(segment.id);
            const frontPad = isActive ? getSegmentFrontPadSeconds(segment) : 0;
            const backPad = isActive ? getSegmentBackPadSeconds(segment) : 0;
            const showInsertBefore =
              showDropMarker &&
              dropMarker?.index === index &&
              dropMarker.placement === 'before';
            const showInsertAfter =
              showDropMarker &&
              dropMarker?.index === index &&
              dropMarker.placement === 'after';

            return (
              <div
                key={segment.id}
                data-copy-id={segment.id}
                className={[
                  'slice-editor-copy-item',
                  isActive ? 'active' : '',
                  isMultiSelected ? 'is-multi-selected' : '',
                  insertAnchorId === segment.id ? 'is-insert-anchor' : '',
                  flashSegmentId === segment.id ? 'slice-editor-copy-item_just-added' : '',
                  dragIdSet.has(segment.id) ? 'slice-editor-copy-item_dragging' : '',
                  showInsertBefore ? 'slice-editor-copy-item_insert-before' : '',
                  showInsertAfter ? 'slice-editor-copy-item_insert-after' : '',
                ]
                  .filter(Boolean)
                  .join(' ')}
                onClick={(event) => handleSegmentSelect(index, event)}
              >
                <div className="slice-editor-copy-item-head">
                  {canDragSort ? (
                    <button
                      type="button"
                      className="slice-editor-copy-drag"
                      title="拖动排序"
                      aria-label="拖动排序"
                      onPointerDown={handleDragHandlePointerDown(index)}
                      onPointerMove={handleDragHandlePointerMove}
                      onPointerUp={handleDragHandlePointerUp}
                      onPointerCancel={handleDragHandlePointerCancel}
                      onClick={(event) => event.stopPropagation()}
                    >
                      <LuGripVertical size={14} />
                    </button>
                  ) : null}
                  <span className="slice-editor-copy-index">片段 {index + 1}</span>
                  <span className="slice-editor-speaker" style={{ color }}>
                    {segment.speakerName}
                  </span>
                  <span className="slice-editor-copy-time">
                    {formatSliceTime(segment.start)} - {formatSliceTime(segment.end)}
                  </span>
                  {flashSegmentId === segment.id ? (
                    <span className="slice-editor-copy-just-added-tag">刚刚加入</span>
                  ) : null}
                  {insertAnchorId === segment.id ? (
                    <span
                      className="slice-editor-copy-insert-anchor-tag"
                      title="左侧新加入的文案会插入到该片段之后"
                    >
                      <LuCornerDownRight size={12} />
                      新内容插入到此之后
                    </span>
                  ) : null}
                </div>

                <p
                  className={`slice-editor-copy-text${isActive ? ' slice-editor-copy-text_active' : ''}`}
                  data-copy-text-id={segment.id}
                  onMouseDown={(event) => {
                    event.stopPropagation();
                    // 组合键点选时阻止浏览器的 Shift 扩展文本选择，否则 click 会被文本选区拦截。
                    if (event.shiftKey || event.metaKey || event.ctrlKey) {
                      event.preventDefault();
                    }
                  }}
                  onMouseUp={(event) => {
                    event.stopPropagation();
                    const target = event.currentTarget;
                    const captureSelection = () => {
                      const offsets = getTextSelectionOffsets(target);
                      textSelectionRef.current = offsets
                        ? { segmentId: segment.id, ...offsets }
                        : null;
                      return offsets;
                    };
                    const offsets = captureSelection();
                    if (!offsets) {
                      requestAnimationFrame(() => {
                        captureSelection();
                      });
                      return;
                    }
                    // 选中文字仅切换当前编辑片段，不覆盖已有多选。
                    selfActiveIdRef.current = segment.id;
                    onActiveSegmentChange(segment.id);
                    onSeek(segment.start);
                  }}
                  onClick={(event) => {
                    event.stopPropagation();
                    const additive = event.shiftKey || event.metaKey || event.ctrlKey;
                    if (!additive) {
                      const selection = window.getSelection();
                      const anchorNode = selection?.anchorNode;
                      // 普通点击且正在选中本片段文字时，保留文本选择，不切换选中态。
                      if (
                        selection &&
                        !selection.isCollapsed &&
                        anchorNode &&
                        event.currentTarget.contains(anchorNode)
                      ) {
                        return;
                      }
                    }

                    handleSegmentSelect(index, event);
                  }}
                >
                  {segment.text}
                </p>

                {isActive && !readOnly && (
                  <div className="slice-editor-copy-actions">
                    <div className="slice-editor-copy-pad" aria-label="调节片段头尾留白">
                      <span className="slice-editor-copy-pad-label">留白</span>
                      <div className="slice-editor-copy-pad-edge">
                        <span className="slice-editor-copy-pad-edge-name">前</span>
                        <button
                          type="button"
                          className="slice-editor-copy-pad-btn"
                          aria-label="前方加留白"
                          disabled={
                            getSegmentAdjustableSeconds(
                              segments,
                              index,
                              'start',
                              'expand',
                              videoDuration,
                              paragraphs
                            ) <= 0
                          }
                          title="前方加留白（按文案分段原始时间，不覆盖前方文字）"
                          onClick={(event) => {
                            event.stopPropagation();
                            onAdjustSegment(segment.id, 'start', SEGMENT_EXTEND_STEP_SEC);
                          }}
                        >
                          <LuPlus size={14} strokeWidth={2.25} />
                        </button>
                        <span
                          className={`slice-editor-copy-pad-value${frontPad < 0.05 ? ' is-zero' : ''}`}
                          title="前方已加留白；只调时间空白，不会带入上一句尾字"
                        >
                          {formatPadSeconds(frontPad)}
                        </span>
                        <button
                          type="button"
                          className="slice-editor-copy-pad-btn"
                          aria-label="收回前方留白"
                          disabled={
                            getSegmentAdjustableSeconds(
                              segments,
                              index,
                              'start',
                              'shrink',
                              videoDuration,
                              paragraphs
                            ) <= 0
                          }
                          title="收回前方留白"
                          onClick={(event) => {
                            event.stopPropagation();
                            onAdjustSegment(segment.id, 'start', -SEGMENT_EXTEND_STEP_SEC);
                          }}
                        >
                          <LuMinus size={14} strokeWidth={2.25} />
                        </button>
                      </div>
                      <span className="slice-editor-copy-pad-sep" aria-hidden="true" />
                      <div className="slice-editor-copy-pad-edge">
                        <span className="slice-editor-copy-pad-edge-name">后</span>
                        <button
                          type="button"
                          className="slice-editor-copy-pad-btn"
                          aria-label={`收回后方留白`}
                          disabled={
                            getSegmentAdjustableSeconds(
                              segments,
                              index,
                              'end',
                              'shrink',
                              videoDuration,
                              paragraphs
                            ) <= 0
                          }
                          title={`收回后方留白`}
                          onClick={(event) => {
                            event.stopPropagation();
                            onAdjustSegment(segment.id, 'end', -SEGMENT_EXTEND_STEP_SEC);
                          }}
                        >
                          <LuMinus size={14} strokeWidth={2.25} />
                        </button>
                        <span
                          className={`slice-editor-copy-pad-value${backPad < 0.05 ? ' is-zero' : ''}`}
                          title={`后方已加留白；只调时间空白，不会带入下一句首字`}
                        >
                          {formatPadSeconds(backPad)}
                        </span>
                        <button
                          type="button"
                          className="slice-editor-copy-pad-btn"
                          aria-label={`后方加留白 ${SEGMENT_EXTEND_STEP_SEC}s`}
                          disabled={
                            getSegmentAdjustableSeconds(
                              segments,
                              index,
                              'end',
                              'expand',
                              videoDuration,
                              paragraphs
                            ) <= 0
                          }
                          title="后方加留白（按文案分段原始时间，不覆盖后方文字）"
                          onClick={(event) => {
                            event.stopPropagation();
                            onAdjustSegment(segment.id, 'end', SEGMENT_EXTEND_STEP_SEC);
                          }}
                        >
                          <LuPlus size={14} strokeWidth={2.25} />
                        </button>
                      </div>
                    </div>
                    <div className="slice-editor-copy-actions-delete-group">
                      <button
                        type="button"
                        onClick={(event) => {
                          event.stopPropagation();
                          onCopySegment(segment.id);
                        }}
                      >
                        <CopyIcon />
                        复制
                      </button>
                      <button
                        type="button"
                        className="danger"
                        onMouseDown={(event) => event.preventDefault()}
                        onClick={(event) => {
                          event.stopPropagation();
                          const textElement = event.currentTarget
                            .closest('.slice-editor-copy-item')
                            ?.querySelector<HTMLElement>(`[data-copy-text-id="${segment.id}"]`) ?? null;
                          const savedSelection =
                            textSelectionRef.current?.segmentId === segment.id
                              ? textSelectionRef.current
                              : null;
                          onDeleteSelectedRange(segment.id, textElement, savedSelection);
                        }}
                      >
                        <LuScissors size={14} />
                        部分删除
                      </button>
                      <button
                        type="button"
                        className="danger"
                        onClick={(event) => {
                          event.stopPropagation();
                          onDeleteSegment(segment.id);
                        }}
                      >
                        <LuTrash2 size={14} />
                        删除
                      </button>
                    </div>
                  </div>
                )}
              </div>
            );
          })
        )}
      </div>
      {segments.length > 0 ? (
        <p className="slice-editor-copy-tip">
          {canDragSort
            ? '拖动左侧把手可调整片段顺序；按住 Ctrl/Cmd 点击可多选，Shift 点击选择连续片段。'
            : ''}
        </p>
      ) : null}

      {dragGhost && draggingSegment
        ? createPortal(
          <div
            className="slice-editor-copy-drag-ghost"
            style={{
              left: dragGhost.x - dragGhost.offsetX,
              top: dragGhost.y - dragGhost.offsetY,
              width: dragGhost.width,
            }}
            aria-hidden
          >
            <div className="slice-editor-copy-drag-ghost-head">
              <LuGripVertical size={14} />
              <span className="slice-editor-copy-index">
                {dragGhost.count > 1 ? `拖动 ${dragGhost.count} 个片段` : `片段 ${dragGhost.index + 1}`}
              </span>
              <span
                className="slice-editor-speaker"
                style={{ color: getSpeakerColor(draggingSegment.speaker, speakers) }}
              >
                {draggingSegment.speakerName}
              </span>
              <span className="slice-editor-copy-time">
                {formatSliceTime(draggingSegment.start)} - {formatSliceTime(draggingSegment.end)}
              </span>
            </div>
            <p className="slice-editor-copy-drag-ghost-text">
              {draggingSegment.text.length > 72
                ? `${draggingSegment.text.slice(0, 72)}…`
                : draggingSegment.text}
            </p>
          </div>,
          document.body
        )
        : null}
    </div>
  );
});

export default SelectedCopyPanel;
