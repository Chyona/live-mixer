import { useState, useEffect } from 'react';

interface OperatingSystemInfo {
  os: 'windows' | 'macos' | 'other';
  isMacArm: boolean;
}

export const useOperatingSystem = (): OperatingSystemInfo => {
  const [osInfo, setOsInfo] = useState<OperatingSystemInfo>({
    os: 'other',
    isMacArm: false,
  });

  useEffect(() => {
    const getOperatingSystem = (): OperatingSystemInfo => {
      // 扩展Navigator接口类型，添加userAgentData支持
      interface NavigatorWithUserAgentData extends Navigator {
        userAgentData?: {
          platform?: string;
        };
      }

      const navigatorWithData = navigator as NavigatorWithUserAgentData;
      if (navigatorWithData.userAgentData && navigatorWithData.userAgentData.platform) {
        const platform = navigatorWithData.userAgentData.platform.toLowerCase();
        if (platform.includes('windows')) {
          return { os: 'windows', isMacArm: false };
        } else if (platform.includes('mac')) {
          // 通过userAgent检测Mac是否为ARM芯片
          const isMacArm = navigator.userAgent.includes('ARM');
          return { os: 'macos', isMacArm };
        }
      } else {
        // 回退到使用userAgent检测（兼容Safari/Firefox等）
        const userAgent = navigator.userAgent.toLowerCase();
        if (userAgent.includes('windows')) {
          return { os: 'windows', isMacArm: false };
        } else if (userAgent.includes('macintosh') || userAgent.includes('mac os x')) {
          const isMacArm = userAgent.includes('arm');
          return { os: 'macos', isMacArm };
        }
      }
      // 其他未知系统
      return { os: 'other', isMacArm: false };
    };

    const osInfo = getOperatingSystem();
    setOsInfo(osInfo);
  }, []);

  return osInfo;
};
