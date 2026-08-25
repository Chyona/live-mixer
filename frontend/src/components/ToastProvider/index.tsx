import { App as AntApp } from 'antd';
import { useEffect } from 'react';
import { registerToast } from '~/utils/toast';

/** 挂载 antd App 上下文，供全局 toast 使用 */
const ToastProvider = ({ children }: { children: React.ReactNode }) => {
  const { message, notification } = AntApp.useApp();

  useEffect(() => {
    registerToast({ message, notification });
    return () => registerToast(null);
  }, [message, notification]);

  return children;
};

export function AppWithToast({ children }: { children: React.ReactNode }) {
  return (
    <AntApp>
      <ToastProvider>{children}</ToastProvider>
    </AntApp>
  );
}

export default AppWithToast;
