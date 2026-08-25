import { lazy, type ComponentType } from 'react';
import type { IconType } from 'react-icons';
import { LuClapperboard, LuExternalLink, LuListTodo, LuMessageSquareText, LuScissors, LuVideo } from 'react-icons/lu';
import { appConfig } from '~/utils/config';

export const DEFAULT_APP_PATH = '/source-videos';

export interface RouteCfgType {
  path: string;
  text: string;
  icon: IconType;
  element: ComponentType | null;  /** 加入公开白名单，未登录可访问；未标记则默认需登录 */
  public?: boolean;
  group?: string;
  href?: string;
  active?: boolean;
  hideInMenu?: boolean;
  iconProps?: React.ComponentProps<IconType>;
  badgeProps?: {
    text: string;
    color?: string;
  };
  links?: {
    text: string;
    icon: IconType;
    href: string;
  }[];
}

/** 已注册页面路由（含 element，非外链） */
export type AppRouteCfg = RouteCfgType & { element: ComponentType };

export function isAppRoute(route: RouteCfgType): route is AppRouteCfg {
  return route.element != null && !route.href;
}

export const LoginRoute = lazy(() => import('~/pages/Login'));
export const SourceVideosPage = lazy(() => import('~/pages/SourceVideos'));
export const AiPromptsPage = lazy(() => import('~/pages/AiPrompts'));
export const SourceVideoSlicePage = lazy(() => import('~/pages/SourceVideoSlice'));
export const ManualVideoSlicePage = lazy(() => import('~/pages/ManualVideoSlice'));
export const SlicesPage = lazy(() => import('~/pages/Slices'));
export const TasksPage = lazy(() => import('~/pages/Tasks'));
export const ErrorPage = lazy(() => import('~/pages/Error'));

/** 导航菜单 & 受保护路由配置，新增页面在此追加 */
export const RoutesCfg: RouteCfgType[] = [
  {
    path: '/source-videos',
    text: '源视频管理',
    icon: LuVideo,
    element: SourceVideosPage,
    iconProps: { strokeWidth: 1.75 },
  },
  {
    path: '/prompts',
    text: '提示词管理',
    icon: LuMessageSquareText,
    element: AiPromptsPage,
    iconProps: { strokeWidth: 1.75 },
  },
  {
    path: '/videos-slice/:id',
    text: '视频切片',
    icon: LuScissors,
    element: SourceVideoSlicePage,
    hideInMenu: true,
    iconProps: { strokeWidth: 1.75 },
  },
  {
    path: '/videos-manual-slice/:id',
    text: '人工切片',
    icon: LuScissors,
    element: ManualVideoSlicePage,
    hideInMenu: true,
    iconProps: { strokeWidth: 1.75 },
  },
  {
    path: '/slices',
    text: '项目管理',
    icon: LuClapperboard,
    element: SlicesPage,
    iconProps: { strokeWidth: 1.75 },
  },
  {
    path: '/tasks',
    text: '任务管理',
    icon: LuListTodo,
    element: TasksPage,
    iconProps: { strokeWidth: 1.75 },
  },
  {
    path: '/jianying-assistant',
    text: '剪映小助手',
    icon: LuExternalLink,
    element: null,
    href: appConfig.jianyingAssistantUrl,
    iconProps: { strokeWidth: 1.75 },
  },
];
