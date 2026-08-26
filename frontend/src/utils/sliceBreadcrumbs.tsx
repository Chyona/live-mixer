import { Link } from 'react-router-dom';
import type { BreadcrumbProps } from 'antd';
import type { SliceEditorEntryFrom } from '~/routes/links';
import { appendDebugAsrKeyToPath } from '~/utils/asrParagraphsKey';

type SlicePageKind = 'timeline' | 'manual';

export const UNSAVED_SLICE_PROJECT_TITLE = '未命名项目';

/** 页面大标题：已保存用项目名，新建未保存用「未命名项目」 */
export function resolveSlicePageTitle(options: {
  projectName?: string;
  saved?: boolean;
}): string {
  if (options.saved) {
    const name = options.projectName?.trim();
    if (name) return name;
  }

  return UNSAVED_SLICE_PROJECT_TITLE;
}

function resolveSlicePageKindLabel(pageKind: SlicePageKind): string {
  return pageKind === 'manual' ? '人工切片' : '时间轴切片';
}

/** 面包屑末级：已保存只显示模式名（项目名在大标题）；未保存显示源视频名 */
function resolveSliceBreadcrumbTitle(options: {
  pageKind: SlicePageKind;
  videoName?: string;
  saved?: boolean;
}): string {
  const { pageKind, videoName, saved } = options;

  if (saved) {
    return resolveSlicePageKindLabel(pageKind);
  }

  if (videoName) {
    return pageKind === 'manual' ? `${videoName} - 人工切片` : `${videoName} - 切片`;
  }

  return resolveSlicePageKindLabel(pageKind);
}

export function buildSliceBreadcrumbItems(options: {
  entryFrom: SliceEditorEntryFrom;
  sourceVideoId: string;
  pageKind: SlicePageKind;
  videoName?: string;
  projectName?: string;
  saved?: boolean;
  onNavigate?: (to: string) => void;
}): BreadcrumbProps['items'] {
  const { entryFrom, pageKind, videoName, saved, onNavigate } = options;
  const currentTitle = resolveSliceBreadcrumbTitle({ pageKind, videoName, saved });

  const renderLink = (to: string, label: string) => {
    const href = appendDebugAsrKeyToPath(to);
    if (onNavigate) {
      return (
        <a
          href={href}
          onClick={(event) => {
            event.preventDefault();
            onNavigate(href);
          }}
        >
          {label}
        </a>
      );
    }

    return <Link to={href}>{label}</Link>;
  };

  if (entryFrom === 'slices') {
    return [{ title: renderLink('/slices', '项目管理') }, { title: currentTitle }];
  }

  if (entryFrom === 'tasks') {
    return [{ title: renderLink('/tasks', '任务管理') }, { title: currentTitle }];
  }

  return [{ title: renderLink('/source-videos', '源视频管理') }, { title: currentTitle }];
}
