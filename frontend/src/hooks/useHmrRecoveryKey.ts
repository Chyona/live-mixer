import { useEffect, useState } from 'react';
import { consumeHmrRecoveryPending } from '~/utils/hmrRecovery';

/**
 * 开发环境：仅在 ErrorBoundary 捕获错误后的下一次 HMR 完成时 remount，
 * 避免 Fast Refresh 后卡在错误页，同时不影响正常热更新。
 */
export function useHmrRecoveryKey() {
  const [key, setKey] = useState(0);

  useEffect(() => {
    if (!import.meta.env.DEV || !import.meta.hot) return;

    const recoverAfterUpdate = () => {
      if (!consumeHmrRecoveryPending()) return;
      setKey((current) => current + 1);
    };

    import.meta.hot.on('vite:afterUpdate', recoverAfterUpdate);

    return () => {
      import.meta.hot?.off('vite:afterUpdate', recoverAfterUpdate);
    };
  }, []);

  return key;
}
