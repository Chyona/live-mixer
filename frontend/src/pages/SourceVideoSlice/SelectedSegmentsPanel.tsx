import { LuX } from 'react-icons/lu';
import DisabledActionWrap from '~/components/DisabledActionWrap';
import TimelineZoomControls from '~/components/VideoTimeline/TimelineZoomControls';
import type { TimeRange } from '~/components/VideoTimeline';
import { formatVideoDuration } from '~/utils/duration';
import '~/components/VideoTimeline/index.css';

function buildActionDisabledReason(options: {
  actionLoading: boolean;
  canAction: boolean;
  selectedRangesCount: number;
  hasSelectedPrompt: boolean;
  isUnderMin: boolean;
  isOverLimit: boolean;
  minTotalDuration: number;
  maxTotalDuration: number;
}) {
  const {
    actionLoading,
    canAction,
    selectedRangesCount,
    hasSelectedPrompt,
    isUnderMin,
    isOverLimit,
    minTotalDuration,
    maxTotalDuration,
  } = options;

  if (actionLoading || canAction) return null;

  const missing: string[] = [];
  if (selectedRangesCount === 0) {
    missing.push('请选中至少一个时间片段');
  }
  if (!hasSelectedPrompt) {
    missing.push('请在上方提示词列表中选择合适的提示词');
  }
  if (isUnderMin) {
    missing.push(`已选时长需不少于 ${minTotalDuration / 60} 分钟`);
  }
  if (isOverLimit) {
    missing.push(`已选时长需不超过 ${maxTotalDuration / 60} 分钟`);
  }

  return missing.length > 0 ? missing.join('；') : null;
}

interface SelectedSegmentsPanelProps {
  videoDuration: number;
  selectedRanges: TimeRange[];
  totalSelectedDuration: number;
  minTotalDuration: number;
  maxTotalDuration: number;
  submitting: boolean;
  aiSelecting: boolean;
  zoomLevel: number;
  onZoomLevelChange: (level: number) => void;
  activeRangeId: string | null;
  onActiveRangeSelect: (rangeId: string, start: number) => void;
  onSubmit: () => void;
  onAiSelect: () => void;
  onClearAll: () => void;
  onRangeDelete: (rangeId: string) => void;
  hasSelectedPrompt: boolean;
  readOnly?: boolean;
}

const SelectedSegmentsPanel = ({
  videoDuration,
  selectedRanges,
  totalSelectedDuration,
  minTotalDuration,
  maxTotalDuration,
  submitting,
  aiSelecting,
  zoomLevel,
  onZoomLevelChange,
  activeRangeId,
  onActiveRangeSelect,
  onSubmit,
  onAiSelect,
  onClearAll,
  onRangeDelete,
  hasSelectedPrompt,
  readOnly = false,
}: SelectedSegmentsPanelProps) => {
  const isUnderMin =
    selectedRanges.length > 0 && totalSelectedDuration < minTotalDuration;
  const isOverLimit = totalSelectedDuration > maxTotalDuration;
  const canAction =
    !readOnly &&
    selectedRanges.length > 0 &&
    hasSelectedPrompt &&
    !isUnderMin &&
    !isOverLimit;
  const actionLoading = submitting || aiSelecting;

  const disabledReason = readOnly
    ? '项目有进行中任务，当前仅可查看'
    : buildActionDisabledReason({
        actionLoading,
        canAction,
        selectedRangesCount: selectedRanges.length,
        hasSelectedPrompt,
        isUnderMin,
        isOverLimit,
        minTotalDuration,
        maxTotalDuration,
      });

  const aiSelectButton = (
    <button
      type="button"
      className="slice-ai-select-btn"
      onClick={onAiSelect}
      disabled={actionLoading || !canAction}
    >
      {aiSelecting ? '选片中...' : 'AI 选片'}
    </button>
  );

  const submitButton = (
    <button
      type="button"
      className="slice-submit-btn"
      onClick={onSubmit}
      disabled={actionLoading || !canAction}
    >
      {submitting ? '处理中...' : '一键成片'}
    </button>
  );

  return (
    <div className="slice-selected-panel">
      <div className="slice-selected-header">
        <div className="slice-selected-header-left">
          <div className="slice-selected-title-row">
            <h3 className="slice-selected-title">
              已选中片段
              <span className="slice-selected-subtitle">
                {readOnly ? '（只读查看）' : '（左键拖拽可继续新增片段）'}
              </span>
            </h3>
            {selectedRanges.length > 0 && (
              <p className="slice-selected-stats">
                已选时长 {formatVideoDuration(totalSelectedDuration)}
                {isUnderMin && (
                  <span className="slice-under-min">
                    （需不少于 {minTotalDuration / 60} 分钟）
                  </span>
                )}
                {isOverLimit && (
                  <span className="slice-over-limit">（超出 {maxTotalDuration / 60} 分钟限制）</span>
                )}
              </p>
            )}
          </div>
        </div>

        <div className="slice-selected-header-right">
          <DisabledActionWrap disabledReason={disabledReason}>{submitButton}</DisabledActionWrap>
          <DisabledActionWrap disabledReason={disabledReason}>{aiSelectButton}</DisabledActionWrap>
          <button
            type="button"
            className="slice-secondary-btn slice-secondary-btn_danger"
            onClick={onClearAll}
            disabled={readOnly || selectedRanges.length === 0 || actionLoading}
          >
            清空
          </button>
        </div>
      </div>

      <div className="slice-selected-toolbar">
        <div
          className={`slice-selected-tags${selectedRanges.length === 0 ? ' slice-selected-tags_empty' : ''}`}
        >
          {selectedRanges.length === 0 ? (
            <span className="slice-selected-empty">暂无选中片段，请在下方时间轴左键拖拽标记</span>
          ) : (
            selectedRanges.map((range, index) => {
              const label = `片段 ${index + 1}`;
              return (
              <button
                key={range.id}
                type="button"
                className={`slice-segment-tag${activeRangeId === range.id ? ' active' : ''}`}
                onClick={() => onActiveRangeSelect(range.id, range.start)}
              >
                <span>
                  {label}: {formatVideoDuration(range.start)} - {formatVideoDuration(range.end)}
                </span>
                {!readOnly ? (
                  <span
                    className="slice-segment-tag-remove"
                    role="button"
                    tabIndex={0}
                    aria-label={`删除${label}`}
                    onClick={(event) => {
                      event.stopPropagation();
                      onRangeDelete(range.id);
                    }}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter' || event.key === ' ') {
                        event.preventDefault();
                        event.stopPropagation();
                        onRangeDelete(range.id);
                      }
                    }}
                  >
                    <LuX size={12} />
                  </span>
                ) : null}
              </button>
              );
            })
          )}
        </div>

        <div className="slice-selected-meta">
          {videoDuration > 0 && (
            <span className="slice-timeline-duration">
              视频总时长 {formatVideoDuration(videoDuration)}
            </span>
          )}
          <div className="slice-selected-zoom">
            <span className="slice-selected-zoom-label">时间轴缩放</span>
            <TimelineZoomControls zoomLevel={zoomLevel} onChange={onZoomLevelChange} />
          </div>
        </div>
      </div>
    </div>
  );
};

export default SelectedSegmentsPanel;
