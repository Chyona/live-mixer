interface TimelineZoomControlsProps {
  zoomLevel: number;
  min?: number;
  max?: number;
  onChange: (level: number) => void;
  className?: string;
}

const TimelineZoomControls = ({
  zoomLevel,
  min = 1,
  max = 10,
  onChange,
  className = '',
}: TimelineZoomControlsProps) => {
  return (
    <div className={`timeline-zoom-controls${className ? ` ${className}` : ''}`}>
      <button
        type="button"
        className="timeline-zoom-btn minus"
        onClick={() => onChange(Math.max(min, zoomLevel - 1))}
        disabled={zoomLevel <= min}
        title="缩小时间线"
        aria-label="缩小时间线"
      >
        <svg width="12" height="12" viewBox="0 0 12 12" fill="none" aria-hidden="true">
          <circle cx="6" cy="6" r="5.5" stroke="currentColor" strokeWidth="1" />
          <line x1="3.5" y1="6" x2="8.5" y2="6" stroke="currentColor" strokeWidth="1" />
        </svg>
      </button>
      <input
        type="range"
        className="timeline-zoom-slider"
        min={min}
        max={max}
        step={1}
        value={zoomLevel}
        onChange={(event) => onChange(Number(event.target.value))}
        aria-label="时间轴缩放"
        aria-valuetext={`${zoomLevel}倍`}
      />
      <span className="timeline-zoom-level">{zoomLevel}X</span>
      <button
        type="button"
        className="timeline-zoom-btn plus"
        onClick={() => onChange(Math.min(max, zoomLevel + 1))}
        disabled={zoomLevel >= max}
        title="放大时间线"
        aria-label="放大时间线"
      >
        <svg width="12" height="12" viewBox="0 0 12 12" fill="none" aria-hidden="true">
          <circle cx="6" cy="6" r="5.5" stroke="currentColor" strokeWidth="1" />
          <line x1="6" y1="3.5" x2="6" y2="8.5" stroke="currentColor" strokeWidth="1" />
          <line x1="3.5" y1="6" x2="8.5" y2="6" stroke="currentColor" strokeWidth="1" />
        </svg>
      </button>
    </div>
  );
};

export default TimelineZoomControls;
