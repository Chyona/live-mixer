import React from 'react';
import TagManager from 'react-gtm-module';

// 初始化 GTM
export const initGTM = (gtmId: string) => {
  // 确保 dataLayer 数组存在
  window.dataLayer = window.dataLayer || [];

  TagManager.initialize({
    gtmId,
    dataLayer: {
      platform: 'web',
      reactVersion: React.version,
    },
  });
};

// 跟踪事件；未启用 GTM 时静默跳过，避免本地控制台刷屏
export const trackEvent = (eventName: string, params: Record<string, any> = {}) => {
  if (!window.dataLayer) {
    return;
  }

  window.dataLayer.push({
    event: eventName,
    ...params,
  });
};
