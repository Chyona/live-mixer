import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Checkbox } from 'antd';
import {
  LuArrowLeft,
  LuCopy,
  LuGripVertical,
  LuMinus,
  LuPlay,
  LuPlus,
  LuScissors,
  LuTextSelect,
  LuTrash2,
} from 'react-icons/lu';
import type { AiSegment, SelectedCopySegment, TranscriptParagraph } from '../types';
import {
  formatPadSeconds,
  formatSliceTime,
  getSegmentAdjustableSeconds,
  getSegmentBackPadSeconds,
  getSegmentFrontPadSeconds,
  getSpeakerColor,
  getTextSelectionOffsets,
  getTotalSelectedDuration,
  reorderSegments,
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

function getReorderToIndex(target: DropMarker, length: number) {
  if (target.placement === 'before') return target.index;
  return Math.min(target.index + 1, length - 1);
}

function wouldReorder(fromIndex: number, target: DropMarker, length: number) {
  let insertIndex = target.placement === 'before' ? target.index : Math.min(target.index + 1, length);
  if (fromIndex < insertIndex) {
    insertIndex -= 1;
  }
  return insertIndex !== fromIndex;
}

interface SelectedCopyPanelProps {
  segments: SelectedCopySegment[];
  /** 文案分段原始数据，用于前后留白边界 */
  paragraphs: TranscriptParagraph[];
  /** AI 分段（asr_summaries），展示在文案预览上方 */
  aiSegments?: AiSegment[];
  currentTime?: number;
  activeSegmentId: string | null;
  speakerIds: string[];
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

const SelectedCopyPanel = ({
  segments,
  paragraphs,
  aiSegments = [],
  currentTime = 0,
  activeSegmentId,
  speakerIds,
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
}: SelectedCopyPanelProps) => {
  const [dragIndex, setDragIndex] = useState<number | null>(null);
  const [dropMarker, setDropMarker] = useState<DropMarker | null>(null);
  const [dragGhost, setDragGhost] = useState<DragGhost | null>(null);
  const [aiPanelHeight, setAiPanelHeight] = useState<number | null>(null);
  const [aiListOverflow, setAiListOverflow] = useState(false);
  const dragIndexRef = useRef<number | null>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const aiBlockRef = useRef<HTMLDivElement>(null);
  const listRef = useRef<HTMLDivElement>(null);
  const pointerDraggingRef = useRef(false);
  const suppressItemClickRef = useRef(false);
  const textSelectionRef = useRef<{ segmentId: string; start: number; end: number } | null>(null);
  const totalDuration = getTotalSelectedDuration(segments);
  const isOverLimit = totalDuration > maxTotalDuration;
  const canDragSort = !readOnly && segments.length > 1;
  const hasSegments = segments.length > 0;
  const showAiResizeHandle = aiListOverflow || aiPanelHeight != null;
  const activeAiSegmentId = useMemo(() => {
    if (!aiSegments.length) return null;
    return (
      aiSegments.find((item) => currentTime >= item.start && currentTime < item.end)?.id ?? null
    );
  }, [aiSegments, currentTime]);
  const draggingSegment = dragIndex != null ? segments[dragIndex] ?? null : null;

  useEffect(() => {
    if (!aiSegments.length) {
      setAiListOverflow(false);
      setAiPanelHeight(null);
      return;
    }

    const block = aiBlockRef.current;
    const list = block?.querySelector<HTMLElement>('.slice-editor-ai-block-list');
    if (!block || !list) return;

    const checkOverflow = () => {
      setAiListOverflow(list.scrollHeight > list.clientHeight + 1);
    };

    checkOverflow();
    const observer = new ResizeObserver(checkOverflow);
    observer.observe(list);
    observer.observe(block);
    return () => observer.disconnect();
  }, [aiSegments, aiPanelHeight]);

  const resetDragState = () => {
    pointerDraggingRef.current = false;
    dragIndexRef.current = null;
    setDragIndex(null);
    setDropMarker(null);
    setDragGhost(null);
  };

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

  const finishPointerDrag = useCallback(
    (clientY: number) => {
      const fromIndex = dragIndexRef.current;
      const target = getDropTarget(clientY);
      if (fromIndex != null && target && wouldReorder(fromIndex, target, segments.length)) {
        const toIndex = getReorderToIndex(target, segments.length);
        onReorder(reorderSegments(segments, fromIndex, toIndex));
        suppressItemClickRef.current = true;
      }
      resetDragState();
    },
    [getDropTarget, onReorder, segments]
  );

  const handleDragHandlePointerDown = (index: number) => (event: React.PointerEvent<HTMLButtonElement>) => {
    if (!canDragSort) return;

    event.preventDefault();
    event.stopPropagation();
    suppressItemClickRef.current = false;
    pointerDraggingRef.current = true;
    dragIndexRef.current = index;
    setDragIndex(index);
    setDropMarker({ index, placement: 'before' });

    const item = event.currentTarget.closest<HTMLElement>('.slice-editor-copy-item');
    const rect = item?.getBoundingClientRect();
    if (rect) {
      setDragGhost({
        index,
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
    if (!pointerDraggingRef.current || dragIndexRef.current == null) return;

    setDragGhost((prev) =>
      prev ? { ...prev, x: event.clientX, y: event.clientY } : prev
    );

    const target = getDropTarget(event.clientY);
    if (!target) return;

    setDropMarker(target);
    if (wouldReorder(dragIndexRef.current, target, segments.length)) {
      suppressItemClickRef.current = true;
    }
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
          dragIndex != null ? 'slice-editor-copy-list_dragging' : '',
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
            const color = getSpeakerColor(segment.speakerId, speakerIds);
            const isActive = activeSegmentId === segment.id;
            const frontPad = isActive ? getSegmentFrontPadSeconds(segment) : 0;
            const backPad = isActive ? getSegmentBackPadSeconds(segment) : 0;
            const showInsertBefore =
              dragIndex != null &&
              dropMarker?.index === index &&
              dropMarker.placement === 'before' &&
              wouldReorder(dragIndex, dropMarker, segments.length);
            const showInsertAfter =
              dragIndex != null &&
              dropMarker?.index === index &&
              dropMarker.placement === 'after' &&
              wouldReorder(dragIndex, dropMarker, segments.length);

            return (
              <div
                key={segment.id}
                className={[
                  'slice-editor-copy-item',
                  isActive ? 'active' : '',
                  dragIndex === index ? 'slice-editor-copy-item_dragging' : '',
                  showInsertBefore ? 'slice-editor-copy-item_insert-before' : '',
                  showInsertAfter ? 'slice-editor-copy-item_insert-after' : '',
                ]
                  .filter(Boolean)
                  .join(' ')}
                onClick={() => {
                  if (suppressItemClickRef.current) {
                    suppressItemClickRef.current = false;
                    return;
                  }
                  if (pointerDraggingRef.current) return;
                  onActiveSegmentChange(segment.id);
                  onSeek(segment.start);
                }}
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
                </div>

                <p
                  className={`slice-editor-copy-text${isActive ? ' slice-editor-copy-text_active' : ''}`}
                  data-copy-text-id={segment.id}
                  onMouseDown={(event) => event.stopPropagation()}
                  onMouseUp={(event) => {
                    event.stopPropagation();
                    const target = event.currentTarget;
                    requestAnimationFrame(() => {
                      const offsets = getTextSelectionOffsets(target);
                      textSelectionRef.current = offsets
                        ? { segmentId: segment.id, ...offsets }
                        : null;
                      if (offsets) {
                        onActiveSegmentChange(segment.id);
                        onSeek(segment.start);
                      }
                    });
                  }}
                  onClick={(event) => {
                    event.stopPropagation();
                    const selection = window.getSelection();
                    if (selection && !selection.isCollapsed) return;

                    onActiveSegmentChange(segment.id);
                    onSeek(segment.start);
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
                        <LuCopy size={14} />
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
            ? '拖动左侧把手可调整片段顺序；单击某一片段可对其进行编辑。'
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
                <span className="slice-editor-copy-index">片段 {dragGhost.index + 1}</span>
                <span
                  className="slice-editor-speaker"
                  style={{ color: getSpeakerColor(draggingSegment.speakerId, speakerIds) }}
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
};

export default SelectedCopyPanel;
