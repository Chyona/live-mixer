import { createContext, useContext, useEffect, useState } from 'react';
import { Navigate, Outlet, useLocation, type Location } from 'react-router-dom';
import PageLoading from '~/components/PageLoading';
import { hasAuthCredentials, UserLoginResult } from '~/services/login';
import { getUserInfo } from '~/services/user';
import { isUnauthorizedError } from '~/services/http';
import { closeLogin, completeLoginRedirect, openLogin } from '~/utils/loginFlow';
import { isLoginModalMode } from '~/utils/config';

/** 非 React 层（如 http / authSession）与 AuthProvider 同步登录态 */
export const LOGIN_CHANGE_EVENT = 'loginStateChange';

/** 清会话后通知 AuthProvider 重置 userInfo */
export function emitAuthLogoutEvent() {
  window.dispatchEvent(new CustomEvent(LOGIN_CHANGE_EVENT, { detail: { state: 'logout' } }));
}

interface AuthContextType {
  loading: boolean;
  userInfo: Partial<UserLoginResult>;
  updateAuthInfo: (tokenData: UserLoginResult) => void;
  fetchUserInfo: (id: string) => Promise<void>;
  logout: () => void;
}

export const AUTH_TOKEN_KEY = 'auth_token';

const AuthContext = createContext<AuthContextType>(null!);

const AuthLoading = () => <PageLoading viewport />;

function readStoredAuth(): Partial<UserLoginResult> | null {
  const raw = localStorage.getItem(AUTH_TOKEN_KEY);
  if (!raw) return null;

  try {
    const parsed = JSON.parse(raw) as Partial<UserLoginResult>;
    if (!hasAuthCredentials(parsed)) {
      localStorage.removeItem(AUTH_TOKEN_KEY);
      return null;
    }
    return parsed;
  } catch {
    localStorage.removeItem(AUTH_TOKEN_KEY);
    return null;
  }
}

function clearLocalAuth() {
  localStorage.removeItem(AUTH_TOKEN_KEY);
}

function ModalLoginGate({ from }: { from: Location }) {
  useEffect(() => {
    openLogin(from);
  }, [from]);

  return <AuthLoading />;
}

export function AuthProvider({ children }: { children: React.ReactNode }) {
  const [userInfo, setUserInfo] = useState<Partial<UserLoginResult>>({});
  const [loading, setLoading] = useState(true);

  useEffect(() => {
    let cancelled = false;

    async function bootstrapAuth() {
      const stored = readStoredAuth();
      if (!stored?.id) {
        if (!cancelled) setLoading(false);
        return;
      }

      // 先写入内存态，便于校验请求带上 Bearer；最终以服务端结果为准
      setUserInfo(stored);

      try {
        const { code, data } = await getUserInfo(stored.id);
        if (cancelled) return;

        if (code === 0 && data) {
          const next: Partial<UserLoginResult> = {
            ...stored,
            ...data,
            // 用户信息接口可能不回传 token，保留本地凭证
            token: data.token || stored.token,
          };
          if (!hasAuthCredentials(next)) {
            clearLocalAuth();
            setUserInfo({});
          } else {
            localStorage.setItem(AUTH_TOKEN_KEY, JSON.stringify(next));
            setUserInfo(next);
          }
        } else {
          clearLocalAuth();
          setUserInfo({});
        }
      } catch (error) {
        // 401 已由拦截器清 session；网络等瞬时错误保留本地凭证，避免误登出
        if (!cancelled && isUnauthorizedError(error)) {
          setUserInfo({});
        }
      } finally {
        if (!cancelled) setLoading(false);
      }
    }

    void bootstrapAuth();
    return () => {
      cancelled = true;
    };
  }, []);

  useEffect(() => {
    const handleLoginChange = (event: Event) => {
      const detail = (event as CustomEvent<{ state?: string }>).detail;
      if (detail?.state === 'logout') {
        setUserInfo({});
      }
      // login：会话已由 updateAuthInfo 写入，事件仅作信号，不携带 token
    };

    window.addEventListener(LOGIN_CHANGE_EVENT, handleLoginChange as EventListener);
    return () => {
      window.removeEventListener(LOGIN_CHANGE_EVENT, handleLoginChange as EventListener);
    };
  }, []);

  const updateAuthInfo = (tokenData: Partial<UserLoginResult>) => {
    let oldData: Partial<UserLoginResult> = {};
    try {
      const val = localStorage.getItem(AUTH_TOKEN_KEY);
      oldData = val ? (JSON.parse(val) as Partial<UserLoginResult>) : {};
    } catch (error) {
      console.error('auth_token JSON old 失败:', error);
    }
    try {
      const newData = {
        ...oldData,
        ...tokenData,
      };
      if (!hasAuthCredentials(newData)) {
        console.error('登录结果缺少 id 或 token，已拒绝写入会话');
        return;
      }
      localStorage.setItem(AUTH_TOKEN_KEY, JSON.stringify(newData));
      setUserInfo(newData);
      closeLogin();
      window.dispatchEvent(new CustomEvent(LOGIN_CHANGE_EVENT, { detail: { state: 'login' } }));
      completeLoginRedirect();
    } catch (error) {
      console.error('auth_token JSON 失败:', error);
    }
  };

  const fetchUserInfo = async (id: string) => {
    const { code, data } = await getUserInfo(id);
    if (code === 0 && data) {
      updateAuthInfo(data);
    }
  };

  const logout = () => {
    clearLocalAuth();
    setUserInfo({});
    emitAuthLogoutEvent();
    openLogin();
  };

  const value: AuthContextType = {
    loading,
    userInfo,
    logout,
    updateAuthInfo,
    fetchUserInfo,
  };

  if (loading) {
    return (
      <AuthContext.Provider value={value}>
        <PageLoading viewport />
      </AuthContext.Provider>
    );
  }

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function ProtectedRoute() {
  const location = useLocation();
  const { userInfo, loading } = useAuth();

  if (loading) return <AuthLoading />;

  if (hasAuthCredentials(userInfo)) return <Outlet />;

  if (isLoginModalMode) {
    return <ModalLoginGate from={location} />;
  }

  return <Navigate to="/login" state={{ from: location }} replace />;
}

export function useAuth() {
  return useContext(AuthContext);
}
