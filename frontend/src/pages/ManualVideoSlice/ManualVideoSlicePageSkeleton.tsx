import type { BreadcrumbProps } from 'antd';
import SlicePageHeader from '~/components/SlicePageHeader';

import './ManualVideoSlicePageSkeleton.css';

interface ManualVideoSlicePageSkeletonProps {
  breadcrumbItems: BreadcrumbProps['items'];
}

const ManualVideoSlicePageSkeleton = ({ breadcrumbItems }: ManualVideoSlicePageSkeletonProps) => {
  return (
    <div
      className="slice-page slice-page_manual"
      aria-busy="true"
      aria-label="人工切片页面加载中"
    >
      <SlicePageHeader breadcrumbItems={breadcrumbItems} title="视频人工切片" />

      <div className="slice-editor-layout manual-slice-loading">
        <div className="slice-editor-main">
          <div className="slice-editor-panel slice-editor-panel_left">
            <div className="manual-slice-loading__video" aria-hidden />
            <div className="manual-slice-loading__divider" aria-hidden />

            <section className="manual-slice-loading__transcript" aria-hidden>
              <div className="manual-slice-loading__transcript-head">
                <div className="manual-slice-loading__line manual-slice-loading__line_title" />
                <div className="manual-slice-loading__pill" />
              </div>
              <div className="manual-slice-loading__search" />
              <div className="manual-slice-loading__transcript-list">
                {Array.from({ length: 5 }).map((_, index) => (
                  <div key={index} className="manual-slice-loading__paragraph">
                    <div className="manual-slice-loading__line manual-slice-loading__line_sm" />
                    <div className="manual-slice-loading__line" />
                    <div className="manual-slice-loading__line manual-slice-loading__line_md" />
                  </div>
                ))}
              </div>
              <p className="manual-slice-loading__status">正在加载视频与文案…</p>
            </section>
          </div>
        </div>

        <aside className="slice-editor-panel slice-editor-panel_copy manual-slice-loading__copy" aria-hidden>
          <div className="manual-slice-loading__copy-head">
            <div className="manual-slice-loading__copy-titles">
              <div className="manual-slice-loading__line manual-slice-loading__line_title" />
              <div className="manual-slice-loading__line manual-slice-loading__line_sm" />
            </div>
            <div className="manual-slice-loading__copy-actions">
              <div className="manual-slice-loading__pill" />
              <div className="manual-slice-loading__pill manual-slice-loading__pill_primary" />
            </div>
          </div>
          <div className="manual-slice-loading__copy-list">
            {Array.from({ length: 4 }).map((_, index) => (
              <div key={index} className="manual-slice-loading__copy-item">
                <div className="manual-slice-loading__line manual-slice-loading__line_sm" />
                <div className="manual-slice-loading__line" />
              </div>
            ))}
          </div>
        </aside>
      </div>
    </div>
  );
};

export default ManualVideoSlicePageSkeleton;
