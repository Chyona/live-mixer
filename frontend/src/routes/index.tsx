import { Suspense, useEffect } from 'react';
import { Navigate, Routes, Route, useNavigate } from 'react-router-dom';
import { setupHttpInterceptors } from '~/services/http';
import { ProtectedRoute } from '~/context/AuthContext';
import { isLoginPageMode } from '~/utils/config';
import { AuthLayout, MainLayout } from '~/layouts';
import PageLoading from '~/components/PageLoading';
import GtmRouterTracker from './gtmRouterTracker';
import NotFound from '~/pages/404';
import { LoginRoute, ErrorPage, RoutesCfg, isAppRoute, DEFAULT_APP_PATH } from './const';

const PageFallback = () => <PageLoading viewport />;

const menuRoutes = RoutesCfg.filter(isAppRoute);
const publicRoutes = menuRoutes.filter((route) => route.public);
const protectedRoutes = menuRoutes.filter((route) => !route.public);

const AppRoutes = () => {
  const navigate = useNavigate();

  useEffect(() => {
    const teardown = setupHttpInterceptors(navigate);
    return teardown;
  }, [navigate]);

  return (
    <>
      <GtmRouterTracker />
      <Suspense fallback={<PageFallback />}>
        <Routes>
          {isLoginPageMode ? (
            <Route element={<AuthLayout />}>
              <Route path="/login" element={<LoginRoute />} />
            </Route>
          ) : (
            <Route path="/login" element={<Navigate to={DEFAULT_APP_PATH} replace />} />
          )}

          <Route element={<MainLayout />}>
            <Route path="/" element={<Navigate to={DEFAULT_APP_PATH} replace />} />
            {publicRoutes.map((route) => {
              const Page = route.element;
              return <Route key={route.path} path={route.path} element={<Page />} />;
            })}
            <Route path="/error" element={<ErrorPage />} />
            <Route element={<ProtectedRoute />}>
              {protectedRoutes.map((route) => {
                const Page = route.element;
                return <Route key={route.path} path={route.path} element={<Page />} />;
              })}
            </Route>
            <Route path="*" element={<NotFound />} />
          </Route>
        </Routes>
      </Suspense>
    </>
  );
};

export default AppRoutes;
