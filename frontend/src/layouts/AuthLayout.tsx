import { Outlet } from 'react-router-dom';

/** 登录 / 注册等无导航壳的页面布局 */
const AuthLayout = () => {
  return <Outlet />;
};

export default AuthLayout;
