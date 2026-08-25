const prefix = (import.meta.env.VITE_API_PREFIX || '/openapi').replace(/\/$/, '');

/** 拼接 API 路径，如 apiPath('/v1/user') => '/openapi/v1/user' */
export const apiPath = (path: string): string => {
  const normalized = path.startsWith('/') ? path : `/${path}`;
  return `${prefix}${normalized}`;
};

export const API_PREFIX = prefix;
