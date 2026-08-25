import { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';

import ErrorBoundary from '~/components/ErrorBoundary';
import AppWithToast from '~/components/ToastProvider';
import { useHmrRecoveryKey } from '~/hooks/useHmrRecoveryKey';
import { AuthProvider } from './context/AuthContext';
import AppRoutes from './routes';
import { setGlobalNavigate } from './utils/navigation';

function App() {
  const navigate = useNavigate();
  const hmrRecoveryKey = useHmrRecoveryKey();

  useEffect(() => {
    setGlobalNavigate(navigate);
    return () => {
      setGlobalNavigate(null);
    };
  }, [navigate]);

  const content = (
    <AppWithToast>
      <AuthProvider>
        <AppRoutes />
      </AuthProvider>
    </AppWithToast>
  );

  return <ErrorBoundary key={import.meta.env.DEV ? hmrRecoveryKey : undefined}>{content}</ErrorBoundary>;
}

export default App;
