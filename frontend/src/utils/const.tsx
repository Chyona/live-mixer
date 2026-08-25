import logoUrl from '~/assets/images/logo.png';

export const DefaultIconUrl = logoUrl;

export const THEME_COLOR = '#5c89ff';

export const isDevelopment = import.meta.env.DEV ?? process.env.NODE_ENV === 'development';
export const isMockEnabled = !!import.meta.env.VITE_MOCK;
export const isProduction = import.meta.env.PROD ?? process.env.NODE_ENV === 'production';

export const AppMinWidth = 1280;

export const ScreenBreakpoint = 992;

export const G_EmptyStr = '-';

/* eslint-disable-next-line */
export const REGEX = /^[^\/&"?<>#{}%~=\'\[\\\]\|\+]*$/;
export const getRegexDesc = (name: string = '该输入项') => {
  /* eslint-disable-next-line */
  return `${name}不允许包含 \/&"?<>#{}%~=\'\[\\\]\|\+ 等特殊字符`;
};
