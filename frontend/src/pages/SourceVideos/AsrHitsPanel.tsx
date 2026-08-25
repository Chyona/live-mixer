import { useState } from 'react';
import { LuChevronDown } from 'react-icons/lu';

import type { LiveAsrSegment } from '~/services/sourceVideo.model';
import { highlightListKeywords } from '~/utils/listKeywords';

interface AsrHitsPanelProps {
  paragraphs: LiveAsrSegment[];
  keywords: string[];
}

const AsrHitsPanel = ({ paragraphs, keywords }: AsrHitsPanelProps) => {
  const [expanded, setExpanded] = useState(true);

  if (!paragraphs.length) return null;

  return (
    <div className={`source-videos-asr-hits${expanded ? '' : ' is-collapsed'}`}>
      <button
        type="button"
        className="source-videos-asr-hits__header"
        aria-expanded={expanded}
        onClick={() => setExpanded((open) => !open)}
      >
        <span className="source-videos-asr-hits__label">命中文案</span>
        <span className="source-videos-asr-hits__count">{paragraphs.length} 段</span>
        <span className="source-videos-asr-hits__toggle">
          {expanded ? '收起' : '展开'}
          <LuChevronDown
            size={14}
            className={`source-videos-asr-hits__chevron${expanded ? ' is-open' : ''}`}
          />
        </span>
      </button>
      {expanded ? (
        <div className="source-videos-asr-hits__body">
          {paragraphs.map((paragraph, index) => (
            <p
              key={`${paragraph.start_time}-${paragraph.end_time}-${index}`}
              className="source-videos-asr-hits__line"
              dangerouslySetInnerHTML={{
                __html: highlightListKeywords(paragraph.text, keywords),
              }}
            />
          ))}
        </div>
      ) : null}
    </div>
  );
};

export default AsrHitsPanel;
