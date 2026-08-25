import type { BreadcrumbProps } from 'antd';
import SlicePageHeader from '~/components/SlicePageHeader';

import TimelineLoadingSkeleton from './TimelineLoadingSkeleton';
import './TimelineLoadingSkeleton.css';
import './SourceVideoSlicePageSkeleton.css';

interface SourceVideoSlicePageSkeletonProps {
  breadcrumbItems: BreadcrumbProps['items'];
}

const SourceVideoSlicePageSkeleton = ({ breadcrumbItems }: SourceVideoSlicePageSkeletonProps) => {
  return (
    <div className="slice-page slice-page_timeline" aria-busy="true" aria-label="视频切片页面加载中">
      <SlicePageHeader breadcrumbItems={breadcrumbItems} title="视频切片" />

      <div className="slice-workspace-card">
        <div className="slice-main-section">
          <div className="slice-video-section">
            <div className="slice-page-loading-video" aria-hidden />
          </div>

          <aside className="slice-page-loading-prompt" aria-hidden>
            <div className="slice-page-loading-prompt__header">
              <div className="slice-page-loading-prompt__title-group">
                <div className="slice-page-loading-prompt__line slice-page-loading-prompt__line_title" />
                <div className="slice-page-loading-prompt__line slice-page-loading-prompt__line_subtitle" />
              </div>
              <div className="slice-page-loading-prompt__actions">
                <div className="slice-page-loading-prompt__pill" />
                <div className="slice-page-loading-prompt__pill slice-page-loading-prompt__pill_primary" />
              </div>
            </div>

            <div className="slice-page-loading-prompt__list">
              {Array.from({ length: 4 }).map((_, index) => (
                <div key={index} className="slice-page-loading-prompt__item">
                  <div className="slice-page-loading-prompt__radio" />
                  <div className="slice-page-loading-prompt__text">
                    <div className="slice-page-loading-prompt__line" />
                    <div className="slice-page-loading-prompt__line slice-page-loading-prompt__line_sm" />
                  </div>
                </div>
              ))}
            </div>
          </aside>
        </div>

        <TimelineLoadingSkeleton
          statusText="正在加载视频切片页面…"
          footerStatusText="准备播放器与时间轴"
        />
      </div>
    </div>
  );
};

export default SourceVideoSlicePageSkeleton;
