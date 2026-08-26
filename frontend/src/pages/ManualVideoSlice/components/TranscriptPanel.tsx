import { Switch, Tooltip } from 'antd';
import { useVirtualizer } from '@tanstack/react-virtual';
import { useCallback, useEffect, useLayoutEffect, useMemo, useRef, useState, type ReactNode } from 'react';
import { highlightPlainKeyword } from '~/utils/listKeywords';
import type { TranscriptParagraph } from '../types';
import KeywordSearchBar from './KeywordSearchBar';
import {
  formatSliceTime,
  getParagraphRange,
  getParagraphText,
  getSpeakerColor,
  paragraphSelectionToCopySegment,
  paragraphToCopySegment,
  scrollElementIntoViewPreferUpper,
  scrollFollowElement,
  splitTextByHighlightRanges,
  type SelectedCopyHighlightRange,
  type TranscriptHighlight,
} from '../utils';

interface TranscriptPanelProps {
  paragraphs: TranscriptParagraph[];
  keyword: string;
  onKeywordChange: (value: string) => void;
  onPrevMatch: () => void;
  onNextMatch: () => void;
  embedded?: boolean;
  activeParagraphId: string | null;
  transcriptHighlight: TranscriptHighlight | null;
  selectedCopyHighlights: SelectedCopyHighlightRange[];
  isVideoPlaying?: boolean;
  activeMatchIndex: number;
  matchParagraphIds: string[];
  onSeek: (time: number) => void;
  onSelectSegment: (segment: ReturnType<typeof paragraphToCopySegment>) => void;
  /** 只读：可定位播放，不可点选/拖选文案 */
  readOnly?: boolean;
}

const TRANSCRIPT_AUTO_SCROLL_KEY = 'manual-slice-transcript-auto-scroll';
/** 超过该段数启用虚拟滚动，避免超长 ASR 列表卡顿 */
const VIRTUAL_SCROLL_THRESHOLD = 80;
const PARAGRAPH_GAP = 10;
const PARAGRAPH_HEAD_HEIGHT = 58;
const PARAGRAPH_LINE_HEIGHT = 26;
const FALLBACK_PARAGRAPH_HEIGHT = 120;

function buildParagraphSizeCacheKey(
  paragraphId: string,
  keyword: string,
  highlightLayoutKey: string
) {
  return `${paragraphId}|${keyword}|${highlightLayoutKey}`;
}

function estimateParagraphHeight(
  paragraph: TranscriptParagraph,
  containerWidth = 480,
  highlightRangeCount = 0
): number {
  const textLen = getParagraphText(paragraph).length;
  const charsPerLine = Math.max(18, Math.floor((containerWidth - 32) / 15));
  const baseLines = Math.max(1, Math.ceil(textLen / charsPerLine));
  const highlightLines = Math.ceil(highlightRangeCount * 0.35);
  const lines = baseLines + highlightLines;
  return PARAGRAPH_HEAD_HEIGHT + lines * PARAGRAPH_LINE_HEIGHT + PARAGRAPH_GAP;
}

function isNodeInContainerViewport(container: HTMLElement, node: HTMLElement) {
  const containerRect = container.getBoundingClientRect();
  const nodeRect = node.getBoundingClientRect();
  return nodeRect.bottom > containerRect.top && nodeRect.top < containerRect.bottom;
}

type TranscriptParagraphItemProps = {
  paragraph: TranscriptParagraph;
  speakers: string[];
  transcriptHighlight: TranscriptHighlight | null;
  selectedCopyHighlights: SelectedCopyHighlightRange[];
  keyword: string;
  isKeywordMatch: boolean;
  isCurrentKeywordMatch: boolean;
  readOnly: boolean;
  onParagraphClick: (event: React.MouseEvent<HTMLDivElement>, paragraph: TranscriptParagraph) => void;
  onParagraphDoubleClick: (event: React.MouseEvent<HTMLDivElement>, paragraph: TranscriptParagraph) => void;
  onTextSelection: (event: React.MouseEvent<HTMLDivElement>, paragraph: TranscriptParagraph) => void;
  onSegmentClick: (event: React.MouseEvent<HTMLSpanElement>, start: number) => void;
};

const TranscriptParagraphItem = ({
  paragraph,
  speakers,
  transcriptHighlight,
  selectedCopyHighlights,
  keyword,
  isKeywordMatch,
  isCurrentKeywordMatch,
  readOnly,
  onParagraphClick,
  onParagraphDoubleClick,
  onTextSelection,
  onSegmentClick,
}: TranscriptParagraphItemProps) => {
  const color = getSpeakerColor(paragraph.speaker, speakers);
  const paragraphRange = getParagraphRange(paragraph);
  const isPlaybackParagraph = transcriptHighlight?.paragraphId === paragraph.id;
  const highlightSegmentIds = new Set(transcriptHighlight?.segmentIds ?? []);
  const paragraphCopyHighlights = selectedCopyHighlights.filter(
    (item) => item.paragraphId === paragraph.id
  );

  const renderParagraphText = () => {
    let clauseOffset = 0;
    const trimmedKeyword = keyword.trim();

    return paragraph.segments.map((segment) => {
      const isPlaybackActive = highlightSegmentIds.has(segment.id);
      const highlightParts = paragraphCopyHighlights.length
        ? splitTextByHighlightRanges(segment.text, clauseOffset, paragraphCopyHighlights)
        : null;
      clauseOffset += segment.text.length;

      let inner: ReactNode;
      if (highlightParts) {
        inner = highlightParts.map((part, index) => {
          const content = trimmedKeyword ? (
            <span
              dangerouslySetInnerHTML={{
                __html: highlightPlainKeyword(part.text, keyword),
              }}
            />
          ) : (
            part.text
          );

          return part.highlighted ? (
            <span key={index} className="segment-selected">
              {content}
            </span>
          ) : (
            <span key={index}>{content}</span>
          );
        });
      } else if (trimmedKeyword) {
        inner = (
          <span
            dangerouslySetInnerHTML={{
              __html: highlightPlainKeyword(segment.text, keyword),
            }}
          />
        );
      } else {
        inner = segment.text;
      }

      return (
        <span
          key={segment.id}
          data-segment-id={segment.id}
          data-start={segment.start}
          data-end={segment.end}
          className={[
            'slice-editor-segment-clause',
            isPlaybackActive ? 'segment-active' : '',
          ]
            .filter(Boolean)
            .join(' ')}
          onClick={(event) => onSegmentClick(event, segment.start)}
        >
          {inner}
        </span>
      );
    });
  };

  return (
    <div
      data-paragraph-id={paragraph.id}
      className={[
        'slice-editor-paragraph',
        isPlaybackParagraph ? 'active' : '',
        isKeywordMatch ? 'matched' : '',
        isCurrentKeywordMatch ? 'matched-current' : '',
      ]
        .filter(Boolean)
        .join(' ')}
      onClick={(event) => onParagraphClick(event, paragraph)}
      onDoubleClick={(event) => onParagraphDoubleClick(event, paragraph)}
    >
      <div className="slice-editor-paragraph-head">
        <div className="slice-editor-paragraph-head-main">
          <span className="slice-editor-speaker" style={{ color }}>
            {paragraph.speakerName}
          </span>
          <span className="slice-editor-paragraph-time">
            {formatSliceTime(paragraphRange.start)} - {formatSliceTime(paragraphRange.end)}
          </span>
        </div>
      </div>
      <div
        className="slice-editor-paragraph-text"
        onMouseUp={(event) => {
          event.stopPropagation();
          onTextSelection(event, paragraph);
        }}
      >
        {renderParagraphText()}
      </div>
    </div>
  );
};

const TranscriptPanel = ({
  paragraphs,
  keyword,
  onKeywordChange,
  onPrevMatch,
  onNextMatch,
  embedded = false,
  activeParagraphId,
  transcriptHighlight,
  selectedCopyHighlights,
  isVideoPlaying = false,
  activeMatchIndex,
  matchParagraphIds,
  onSeek,
  onSelectSegment,
  readOnly = false,
}: TranscriptPanelProps) => {
  const transcriptBodyRef = useRef<HTMLDivElement>(null);
  const lastAutoScrolledTargetRef = useRef<string | null>(null);
  const autoScrollPauseTimerRef = useRef<number>(0);
  const pendingSeekTimerRef = useRef<number>(0);
  const suppressNextClickSeekRef = useRef(false);
  const [autoScrollPaused, setAutoScrollPaused] = useState(false);
  const [autoScrollEnabled, setAutoScrollEnabled] = useState(() => {
    return localStorage.getItem(TRANSCRIPT_AUTO_SCROLL_KEY) !== 'false';
  });

  const speakers = useMemo(
    () => [...new Set(paragraphs.map((item) => item.speaker))],
    [paragraphs]
  );

  const paragraphSyncKey = useMemo(
    () => paragraphs.map((paragraph) => paragraph.id).join('|'),
    [paragraphs]
  );

  const trimmedKeyword = keyword.trim();

  const paragraphIndexById = useMemo(() => {
    const map = new Map<string, number>();
    paragraphs.forEach((paragraph, index) => {
      map.set(paragraph.id, index);
    });
    return map;
  }, [paragraphs]);

  const highlightLayoutKey = useMemo(
    () =>
      selectedCopyHighlights
        .map((item) => `${item.paragraphId}:${item.charStart}-${item.charEnd}`)
        .join('|'),
    [selectedCopyHighlights]
  );

  const highlightCountByParagraphId = useMemo(() => {
    const map = new Map<string, number>();
    for (const item of selectedCopyHighlights) {
      map.set(item.paragraphId, (map.get(item.paragraphId) ?? 0) + 1);
    }
    return map;
  }, [selectedCopyHighlights]);

  const paragraphSizeCacheRef = useRef<Map<string, number>>(new Map());
  const containerWidthRef = useRef(480);
  const shouldVirtualize = paragraphs.length > VIRTUAL_SCROLL_THRESHOLD;

  useEffect(() => {
    paragraphSizeCacheRef.current.clear();
  }, [paragraphSyncKey, highlightLayoutKey, trimmedKeyword]);

  useEffect(() => {
    const container = transcriptBodyRef.current;
    if (!container) return;

    const updateWidth = () => {
      containerWidthRef.current = container.clientWidth || 480;
    };

    updateWidth();
    const observer = new ResizeObserver(updateWidth);
    observer.observe(container);
    return () => observer.disconnect();
  }, [paragraphSyncKey]);

  const playbackSegmentId = transcriptHighlight?.segmentIds[0] ?? '';
  const playbackHighlightMode = transcriptHighlight?.mode ?? null;

  const getParagraphSizeCacheKey = useCallback(
    (paragraphId: string) =>
      buildParagraphSizeCacheKey(paragraphId, trimmedKeyword, highlightLayoutKey),
    [highlightLayoutKey, trimmedKeyword]
  );

  const virtualizer = useVirtualizer({
    count: paragraphs.length,
    getScrollElement: () => transcriptBodyRef.current,
    estimateSize: (index) => {
      const paragraph = paragraphs[index];
      if (!paragraph) return FALLBACK_PARAGRAPH_HEIGHT + PARAGRAPH_GAP;
      const cacheKey = getParagraphSizeCacheKey(paragraph.id);
      const cached = paragraphSizeCacheRef.current.get(cacheKey);
      return (
        cached ??
        estimateParagraphHeight(
          paragraph,
          containerWidthRef.current,
          highlightCountByParagraphId.get(paragraph.id) ?? 0
        )
      );
    },
    measureElement: (element) => {
      if (!(element instanceof HTMLElement)) {
        return FALLBACK_PARAGRAPH_HEIGHT + PARAGRAPH_GAP;
      }
      // padding-bottom 段间距计入 offsetHeight，与 flex gap 一致
      const height = element.offsetHeight;
      const index = Number(element.getAttribute('data-index'));
      const paragraph = paragraphs[index];
      if (paragraph && height > 0) {
        paragraphSizeCacheRef.current.set(getParagraphSizeCacheKey(paragraph.id), height);
      }
      return height;
    },
    overscan: 6,
    getItemKey: (index) => {
      const paragraph = paragraphs[index];
      if (!paragraph) return String(index);
      return getParagraphSizeCacheKey(paragraph.id);
    },
  });

  useLayoutEffect(() => {
    if (!shouldVirtualize) return;
    virtualizer.measure();
  }, [
    shouldVirtualize,
    highlightLayoutKey,
    trimmedKeyword,
    paragraphSyncKey,
    virtualizer,
  ]);

  const scrollToParagraphIndex = useCallback(
    (index: number, behavior: ScrollBehavior = 'smooth') => {
      if (index < 0 || index >= paragraphs.length) return;

      const paragraph = paragraphs[index];
      if (!paragraph) return;

      if (shouldVirtualize) {
        virtualizer.scrollToIndex(index, { align: 'auto', behavior });
      }

      requestAnimationFrame(() => {
        requestAnimationFrame(() => {
          const container = transcriptBodyRef.current;
          if (!container) return;

          const node = container.querySelector<HTMLElement>(`[data-paragraph-id="${paragraph.id}"]`);
          if (node) {
            scrollElementIntoViewPreferUpper(container, node, { behavior });
          }
        });
      });
    },
    [paragraphs, shouldVirtualize, virtualizer]
  );

  const scrollToParagraphId = useCallback(
    (paragraphId: string, behavior: ScrollBehavior = 'smooth') => {
      const index = paragraphIndexById.get(paragraphId);
      if (index == null) return;
      scrollToParagraphIndex(index, behavior);
    },
    [paragraphIndexById, scrollToParagraphIndex]
  );

  const pauseAutoScroll = useCallback(() => {
    if (!autoScrollEnabled) return;

    setAutoScrollPaused(true);
    window.clearTimeout(autoScrollPauseTimerRef.current);
    autoScrollPauseTimerRef.current = window.setTimeout(() => {
      setAutoScrollPaused(false);
      lastAutoScrolledTargetRef.current = null;
    }, 2500);
  }, [autoScrollEnabled]);

  const handleAutoScrollEnabledChange = (checked: boolean) => {
    setAutoScrollEnabled(checked);
    localStorage.setItem(TRANSCRIPT_AUTO_SCROLL_KEY, String(checked));
    setAutoScrollPaused(false);
    lastAutoScrolledTargetRef.current = null;
  };

  useEffect(() => {
    return () => {
      window.clearTimeout(autoScrollPauseTimerRef.current);
      window.clearTimeout(pendingSeekTimerRef.current);
    };
  }, []);

  const scheduleSeek = useCallback(
    (time: number) => {
      window.clearTimeout(pendingSeekTimerRef.current);
      pendingSeekTimerRef.current = window.setTimeout(() => {
        onSeek(time);
      }, 220);
    },
    [onSeek]
  );

  const cancelPendingSeek = useCallback(() => {
    window.clearTimeout(pendingSeekTimerRef.current);
    pendingSeekTimerRef.current = 0;
  }, []);

  useEffect(() => {
    if (!autoScrollEnabled || autoScrollPaused || !activeParagraphId) return;
    if (playbackHighlightMode !== 'playback') return;
    if (!isVideoPlaying) return;

    const scrollTargetKey = `${activeParagraphId}:${playbackSegmentId}`;
    if (lastAutoScrolledTargetRef.current === scrollTargetKey) return;

    const index = paragraphIndexById.get(activeParagraphId);
    if (index == null) return;

    const container = transcriptBodyRef.current;
    if (!container) return;

    lastAutoScrolledTargetRef.current = scrollTargetKey;

    const applyFollowScroll = () => {
      const segmentNode = playbackSegmentId
        ? container.querySelector<HTMLElement>(`[data-segment-id="${playbackSegmentId}"]`)
        : null;
      const paragraphNode = container.querySelector<HTMLElement>(
        `[data-paragraph-id="${activeParagraphId}"]`
      );
      const target = segmentNode ?? paragraphNode;
      if (!target) return false;
      scrollFollowElement(container, target, { behavior: 'smooth' });
      return true;
    };

    const paragraphNode = container.querySelector<HTMLElement>(
      `[data-paragraph-id="${activeParagraphId}"]`
    );

    if (!paragraphNode || !isNodeInContainerViewport(container, paragraphNode)) {
      if (shouldVirtualize) {
        virtualizer.scrollToIndex(index, { align: 'auto', behavior: 'smooth' });
      } else {
        scrollToParagraphIndex(index, 'smooth');
      }
      requestAnimationFrame(() => {
        requestAnimationFrame(applyFollowScroll);
      });
      return;
    }

    applyFollowScroll();
  }, [
    activeParagraphId,
    autoScrollEnabled,
    autoScrollPaused,
    isVideoPlaying,
    paragraphIndexById,
    playbackHighlightMode,
    playbackSegmentId,
    scrollToParagraphIndex,
    shouldVirtualize,
    virtualizer,
  ]);

  useEffect(() => {
    lastAutoScrolledTargetRef.current = null;
  }, [paragraphSyncKey]);

  useEffect(() => {
    if (!keyword.trim() || matchParagraphIds.length === 0) return;

    const paragraphId = matchParagraphIds[activeMatchIndex];
    if (!paragraphId) return;

    scrollToParagraphId(paragraphId);

    const paragraph = paragraphs.find((item) => item.id === paragraphId);
    if (paragraph?.segments[0]) {
      onSeek(paragraph.segments[0].start);
    }
  }, [
    activeMatchIndex,
    keyword,
    matchParagraphIds,
    onSeek,
    paragraphs,
    scrollToParagraphId,
  ]);

  const handleParagraphClick = (
    event: React.MouseEvent<HTMLDivElement>,
    paragraph: TranscriptParagraph
  ) => {
    if (event.detail > 1) return;
    if (suppressNextClickSeekRef.current) {
      suppressNextClickSeekRef.current = false;
      return;
    }

    const range = getParagraphRange(paragraph);
    scheduleSeek(range.start);
  };

  const handleParagraphDoubleClick = (
    event: React.MouseEvent<HTMLDivElement>,
    paragraph: TranscriptParagraph
  ) => {
    if (readOnly) return;
    event.preventDefault();
    event.stopPropagation();
    cancelPendingSeek();
    suppressNextClickSeekRef.current = false;
    window.getSelection()?.removeAllRanges();

    const copySegment = paragraphToCopySegment(paragraph);
    if (copySegment) {
      onSelectSegment(copySegment);
    }
  };

  const handleTextSelection = (
    event: React.MouseEvent<HTMLDivElement>,
    paragraph: TranscriptParagraph
  ) => {
    if (readOnly) return;
    if (event.detail >= 2) return;

    const selection = window.getSelection();
    if (!selection || selection.isCollapsed) return;

    const copySegment = paragraphSelectionToCopySegment(event.currentTarget, paragraph);
    if (copySegment) {
      suppressNextClickSeekRef.current = true;
      cancelPendingSeek();
      onSelectSegment(copySegment);
      selection.removeAllRanges();
    }
  };

  const handleSegmentClick = (event: React.MouseEvent<HTMLSpanElement>, start: number) => {
    event.stopPropagation();
    if (event.detail > 1) return;
    if (suppressNextClickSeekRef.current) {
      suppressNextClickSeekRef.current = false;
      return;
    }
    scheduleSeek(start);
  };

  const renderParagraphItem = (
    paragraph: TranscriptParagraph,
    options: {
      isKeywordMatch: boolean;
      isCurrentKeywordMatch: boolean;
    }
  ) => (
    <TranscriptParagraphItem
      paragraph={paragraph}
      speakers={speakers}
      transcriptHighlight={transcriptHighlight}
      selectedCopyHighlights={selectedCopyHighlights}
      keyword={keyword}
      isKeywordMatch={options.isKeywordMatch}
      isCurrentKeywordMatch={options.isCurrentKeywordMatch}
      readOnly={readOnly}
      onParagraphClick={handleParagraphClick}
      onParagraphDoubleClick={handleParagraphDoubleClick}
      onTextSelection={handleTextSelection}
      onSegmentClick={handleSegmentClick}
    />
  );

  const virtualItems = shouldVirtualize ? virtualizer.getVirtualItems() : [];

  return (
    <div
      className={
        embedded
          ? 'slice-editor-transcript-section'
          : 'slice-editor-panel slice-editor-panel_transcript'
      }
    >
      <div className="slice-editor-transcript-top">
        <div className="slice-editor-transcript-head">
          <div className="slice-editor-transcript-head-main">
            <div className="slice-editor-panel-title">文案分段</div>
            <Tooltip title="开启后，播放视频时文案列表会自动滚动，将当前朗读段落居中显示">
              <label className="slice-editor-transcript-follow">
                <Switch
                  size="small"
                  checked={autoScrollEnabled}
                  onChange={handleAutoScrollEnabledChange}
                />
                <span>定位跟随</span>
              </label>
            </Tooltip>
          </div>
          <KeywordSearchBar
            value={keyword}
            onChange={onKeywordChange}
            matchCount={matchParagraphIds.length}
            activeMatchIndex={activeMatchIndex}
            onPrevMatch={onPrevMatch}
            onNextMatch={onNextMatch}
          />
        </div>
      </div>
      <div
        ref={transcriptBodyRef}
        className={
          shouldVirtualize
            ? 'slice-editor-transcript-body slice-editor-transcript-body_virtual'
            : 'slice-editor-transcript-body'
        }
        onWheel={pauseAutoScroll}
        onTouchStart={pauseAutoScroll}
        onMouseDown={pauseAutoScroll}
      >
        {shouldVirtualize ? (
          <div
            className="slice-editor-transcript-virtual-inner"
            style={{ height: virtualizer.getTotalSize() }}
          >
            {virtualItems.map((virtualItem) => {
              const paragraph = paragraphs[virtualItem.index];
              if (!paragraph) return null;

              const isKeywordMatch = matchParagraphIds.includes(paragraph.id);
              const keywordMatchIndex = matchParagraphIds.indexOf(paragraph.id);
              const isCurrentKeywordMatch =
                Boolean(trimmedKeyword) &&
                isKeywordMatch &&
                keywordMatchIndex === activeMatchIndex;

              return (
                <div
                  key={virtualItem.key}
                  data-index={virtualItem.index}
                  ref={virtualizer.measureElement}
                  className="slice-editor-transcript-virtual-item"
                  style={{ transform: `translateY(${virtualItem.start}px)` }}
                >
                  {renderParagraphItem(paragraph, { isKeywordMatch, isCurrentKeywordMatch })}
                </div>
              );
            })}
          </div>
        ) : (
          paragraphs.map((paragraph) => {
            const isKeywordMatch = matchParagraphIds.includes(paragraph.id);
            const keywordMatchIndex = matchParagraphIds.indexOf(paragraph.id);
            const isCurrentKeywordMatch =
              Boolean(trimmedKeyword) && isKeywordMatch && keywordMatchIndex === activeMatchIndex;

            return (
              <div key={paragraph.id}>
                {renderParagraphItem(paragraph, { isKeywordMatch, isCurrentKeywordMatch })}
              </div>
            );
          })
        )}
      </div>
      <p className="slice-editor-transcript-tip">
        {readOnly
          ? '当前为只读模式，单击可定位视频，暂不可选择文案。'
          : '单击定位视频，双击选中整段；拖拽选中部分文字可提取对应片段。'}
      </p>
    </div>
  );
};

export default TranscriptPanel;
