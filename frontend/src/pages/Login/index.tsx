import { useEffect } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';
import LoginModal from '~/components/LoginModal';
import { useAppSEO } from '~/hooks/useAppSEO';
import { AUTH_TOKEN_KEY, useAuth } from '~/context/AuthContext';
import { hasAuthCredentials } from '~/services/login';
import { buildReturnPath, openLogin, type LoginFrom } from '~/utils/loginFlow';
import { DEFAULT_APP_PATH } from '~/routes/const';

import './index.css';

function hasStoredAuthCredentials(): boolean {
  try {
    const raw = localStorage.getItem(AUTH_TOKEN_KEY);
    if (!raw) return false;
    return hasAuthCredentials(JSON.parse(raw));
  } catch {
    return false;
  }
}

const LoginPage = () => {
  const location = useLocation();
  const navigate = useNavigate();
  const { userInfo } = useAuth();

  useAppSEO({
    title: '登录',
    path: '/login',
    description: '登录您的账户',
    robots: 'noindex, nofollow',
    twitterCard: 'summary',
  });

  useEffect(() => {
    openLogin(location);
  }, [location]);

  useEffect(() => {
    // 会话刚被清掉时 React state 可能尚未同步，勿用残留 userInfo 踢回业务页
    if (!hasAuthCredentials(userInfo) || !hasStoredAuthCredentials()) return;

    const from = (location.state as { from?: LoginFrom })?.from;
    const returnTo = from?.pathname ? buildReturnPath(from) : DEFAULT_APP_PATH;
    navigate(returnTo, { replace: true });
  }, [userInfo, location.state, navigate]);

  return (
    <div className="login-page">
      <div className="login-page-bg" aria-hidden>
        <div className="login-page-bg__base" />
        <div className="login-page-bg__mesh" />
        <div className="login-page-bg__blob login-page-bg__blob_purple" />
        <div className="login-page-bg__blob login-page-bg__blob_lavender" />
        <div className="login-page-bg__blob login-page-bg__blob_pink" />
        <div className="login-page-bg__blob login-page-bg__blob_blue" />
      </div>

      <div className="login-page-content">
        <LoginModal closeIcon={false} mask={false} />
      </div>
    </div>
  );
};

export default LoginPage;
