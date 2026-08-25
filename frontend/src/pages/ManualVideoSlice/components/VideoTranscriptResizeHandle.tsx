import { useCallback, useState } from 'react';
import { LuRotateCcw } from 'react-icons/lu';

interface VideoTranscriptResizeHandleProps {
  isCustomized: boolean;
  onResize: (height: number) => void;
  onMeasureStart: () => number;
  onMeasurePanel: () => number;
  /** 可选：内容刚好铺满、无需滚动时的高度上限（避免拖出大片空白） */
  onMeasureContentMax?: () => number;
  onReset: () => void;
  minHeight?: number;
  maxHeightRatio?: number;
  ariaLabel?: string;
  title?: string;
}

const VideoTranscriptResizeHandle = ({
  isCustomized,
  onResize,
  onMeasureStart,
  onMeasurePanel,
  onMeasureContentMax,
  onReset,
  minHeight = 120,
  maxHeightRatio = 0.78,
  ariaLabel = '拖动调整上方区域高度，双击还原',
  title = '拖动调整高度，双击还原',
}: VideoTranscriptResizeHandleProps) => {
  const [active, setActive] = useState(false);

  const resolveMaxHeight = useCallback(() => {
    const panelHeight = onMeasurePanel();
    const ratioMax = panelHeight * maxHeightRatio;
    const contentMax = onMeasureContentMax?.() ?? Number.POSITIVE_INFINITY;
    return Math.max(minHeight, Math.min(ratioMax, contentMax));
  }, [maxHeightRatio, minHeight, onMeasureContentMax, onMeasurePanel]);

  const handleMouseDown = useCallback(
    (event: React.MouseEvent<HTMLDivElement>) => {
      event.preventDefault();

      const startY = event.clientY;
      const startHeight = onMeasureStart();
      const maxHeight = resolveMaxHeight();

      setActive(true);
      document.body.style.cursor = 'ns-resize';
      document.body.style.userSelect = 'none';

      const handleMouseMove = (moveEvent: MouseEvent) => {
        const nextHeight = Math.min(
          maxHeight,
          Math.max(minHeight, startHeight + moveEvent.clientY - startY)
        );
        onResize(nextHeight);
      };

      const handleMouseUp = () => {
        setActive(false);
        document.body.style.cursor = '';
        document.body.style.userSelect = '';
        document.removeEventListener('mousemove', handleMouseMove);
        document.removeEventListener('mouseup', handleMouseUp);
      };

      document.addEventListener('mousemove', handleMouseMove);
      document.addEventListener('mouseup', handleMouseUp);
    },
    [minHeight, onMeasureStart, onResize, resolveMaxHeight]
  );

  const handleReset = (event: React.MouseEvent<HTMLButtonElement>) => {
    event.preventDefault();
    event.stopPropagation();
    onReset();
  };

  const handleDoubleClick = () => {
    onReset();
  };

  return (
    <div
      className={[
        'slice-editor-section-divider',
        'slice-editor-section-divider_resizable',
        active ? 'slice-editor-section-divider_active' : '',
        isCustomized ? 'slice-editor-section-divider_customized' : '',
      ]
        .filter(Boolean)
        .join(' ')}
      role="separator"
      aria-orientation="horizontal"
      aria-label={ariaLabel}
    >
      <div
        className="slice-editor-section-divider-hit"
        title={title}
        onMouseDown={handleMouseDown}
        onDoubleClick={handleDoubleClick}
      />
      <div className="slice-editor-section-divider-grip" aria-hidden>
        <span />
        <span />
      </div>
      {isCustomized ? (
        <button
          type="button"
          className="slice-editor-section-divider-reset"
          title="还原高度"
          aria-label="还原高度"
          onClick={handleReset}
          onMouseDown={(event) => event.stopPropagation()}
        >
          <LuRotateCcw size={12} />
        </button>
      ) : null}
    </div>
  );
};

export default VideoTranscriptResizeHandle;
