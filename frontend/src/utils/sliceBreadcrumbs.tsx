import { Link } from 'react-router-dom';
import type { BreadcrumbProps } from 'antd';
import type { SliceEditorEntryFrom } from '~/routes/links';

type SlicePageKind = 'timeline' | 'manual';

export function buildSliceBreadcrumbItems(options: {
  entryFrom: SliceEditorEntryFrom;
  sourceVideoId: string;
  pageKind: SlicePageKind;
  videoName?: string;
}): BreadcrumbProps['items'] {
  const { entryFrom, sourceVideoId, pageKind, videoName } = options;
  const currentTitle =
    pageKind === 'manual'
      ? videoName
        ? `${videoName} - 人工切片`
        : '人工切片'
      : videoName
        ? `${videoName} - 切片`
        : '视频切片';

  if (entryFrom === 'slices') {
    return [{ title: <Link to="/slices">项目管理</Link> }, { title: currentTitle }];
  }

  if (entryFrom === 'tasks') {
    return [{ title: <Link to="/tasks">任务管理</Link> }, { title: currentTitle }];
  }

  return [{ title: <Link to="/source-videos">源视频管理</Link> }, { title: currentTitle }];
}
