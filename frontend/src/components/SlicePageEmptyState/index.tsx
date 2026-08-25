import { Button } from 'antd';
import { Link } from 'react-router-dom';
import {
  LuArrowLeft,
  LuCircleAlert,
  LuFileWarning,
  LuLink2Off,
  LuRotateCw,
} from 'react-icons/lu';
import type { SliceEditorEntryFrom } from '~/routes/links';

import './index.css';

export type SlicePageEmptyVariant = 'video-unavailable' | 'no-playback-url' | 'unsupported-format';

interface SlicePageEmptyStateProps {
  variant: SlicePageEmptyVariant;
  entryFrom?: SliceEditorEntryFrom;
}

const variantConfig = {
  'video-unavailable': {
    icon: LuCircleAlert,
    tone: 'warning',
    title: '无法打开该源视频',
    description: '视频可能已被删除、链接已失效，或当前账号无权访问。',
    reasons: ['请确认地址栏中的视频 ID 是否正确', '检查该视频是否仍在源视频列表中', '如为分享访问，请确认账号权限'],
  },
  'no-playback-url': {
    icon: LuLink2Off,
    tone: 'info',
    title: '暂无可用播放地址',
    description: '该源视频尚未配置可播放链接，补充播放地址后即可开始切片。',
    reasons: ['前往源视频管理查看并编辑该视频', '确认已上传或填写有效的播放链接'],
  },
  'unsupported-format': {
    icon: LuFileWarning,
    tone: 'neutral',
    title: '播放地址格式不受支持',
    description: '当前链接无法在浏览器中直接播放，请更换为常见流媒体格式。',
    reasons: ['推荐使用 m3u8、mp4 等可播放链接', '更换地址后刷新页面重试'],
  },
} as const;

function getBackAction(entryFrom: SliceEditorEntryFrom = 'source-videos') {
  if (entryFrom === 'slices') {
    return { to: '/slices', label: '返回项目管理' };
  }

  if (entryFrom === 'tasks') {
    return { to: '/tasks', label: '返回任务管理' };
  }

  return { to: '/source-videos', label: '返回源视频管理' };
}

const SlicePageEmptyState = ({ variant, entryFrom = 'source-videos' }: SlicePageEmptyStateProps) => {
  const config = variantConfig[variant];
  const Icon = config.icon;
  const backAction = getBackAction(entryFrom);

  return (
    <div className="slice-page-empty">
      <div className="slice-page-empty-card">
        <div className={`slice-page-empty-icon slice-page-empty-icon_${config.tone}`} aria-hidden>
          <Icon size={28} strokeWidth={1.6} />
        </div>
        <h2 className="slice-page-empty-title">{config.title}</h2>
        <p className="slice-page-empty-desc">{config.description}</p>
        <ul className="slice-page-empty-reasons">
          {config.reasons.map((reason) => (
            <li key={reason}>{reason}</li>
          ))}
        </ul>
        <div className="slice-page-empty-actions">
          <Link to={backAction.to}>
            <Button type="primary" icon={<LuArrowLeft size={14} />}>
              {backAction.label}
            </Button>
          </Link>
          <Button icon={<LuRotateCw size={14} />} onClick={() => window.location.reload()}>
            刷新重试
          </Button>
        </div>
      </div>
    </div>
  );
};

export default SlicePageEmptyState;
