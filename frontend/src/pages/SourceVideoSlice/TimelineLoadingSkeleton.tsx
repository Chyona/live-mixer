import './TimelineLoadingSkeleton.css';

interface TimelineLoadingSkeletonProps {
  statusText?: string;
  footerStatusText?: string;
}

const TimelineLoadingSkeleton = ({
  statusText = '视频加载中，时间轴准备就绪…',
  footerStatusText = '解析视频时长',
}: TimelineLoadingSkeletonProps) => {
  return (
    <div className="slice-timeline-loading" aria-busy="true" aria-label="时间轴加载中">
      <div className="slice-timeline-loading__panel">
        <div className="slice-timeline-loading__header">
          <div className="slice-timeline-loading__title-group">
            <div className="slice-timeline-loading__line slice-timeline-loading__line_title" />
            <div className="slice-timeline-loading__line slice-timeline-loading__line_subtitle" />
          </div>
          <div className="slice-timeline-loading__actions">
            <div className="slice-timeline-loading__pill slice-timeline-loading__pill_primary" />
            <div className="slice-timeline-loading__pill" />
            <div className="slice-timeline-loading__pill" />
          </div>
        </div>

        <div className="slice-timeline-loading__toolbar">
          <div className="slice-timeline-loading__tags">
            {Array.from({ length: 3 }).map((_, index) => (
              <div
                key={index}
                className="slice-timeline-loading__tag-pill"
                style={{ animationDelay: `${index * 0.12}s` }}
              />
            ))}
          </div>
          <div className="slice-timeline-loading__pill slice-timeline-loading__pill_sm" />
        </div>
        <p className="slice-timeline-loading__status">{statusText}</p>
      </div>

      <div className="slice-timeline-loading__timeline">
        <div className="slice-timeline-loading__ruler">
          {Array.from({ length: 13 }).map((_, index) => (
            <span
              key={index}
              className="slice-timeline-loading__tick"
              style={{ animationDelay: `${index * 0.08}s` }}
            />
          ))}
        </div>

        <div className="slice-timeline-loading__track">
          <div className="slice-timeline-loading__track-shimmer" />
          <div className="slice-timeline-loading__track-wave" />
          <div className="slice-timeline-loading__playhead">
            <span className="slice-timeline-loading__playhead-line" />
            <span className="slice-timeline-loading__playhead-dot" />
          </div>
        </div>

        <div className="slice-timeline-loading__footer">
          <span>0:00</span>
          <span className="slice-timeline-loading__footer-status">{footerStatusText}</span>
          <span>--:--</span>
        </div>
      </div>
    </div>
  );
};

export default TimelineLoadingSkeleton;
