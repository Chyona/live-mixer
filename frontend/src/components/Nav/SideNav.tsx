import { Button, Layout, Tooltip } from 'antd';
import { MenuFoldOutlined, MenuUnfoldOutlined } from '@ant-design/icons';
import { Link } from 'react-router-dom';
import Logo_src from '~/assets/images/logo.png';
import { appConfig } from '~/utils/config';
import { DEFAULT_APP_PATH } from '~/routes/const';
import NavMenuItems from './NavMenuItems';
import UserActions from './UserActions';
import { useNavItems } from './useNavItems';

import './index.css';

const { Sider } = Layout;

const SIDER_WIDTH = 248;
const SIDER_COLLAPSED_WIDTH = 72;

type SideNavProps = {
  collapsed: boolean;
  onCollapse: (collapsed: boolean) => void;
};

const SideNav = ({ collapsed, onCollapse }: SideNavProps) => {
  const { navItems, rotatedItemKey, setRotatedItemKey } = useNavItems();

  return (
    <Sider
      className="zt-sider"
      width={SIDER_WIDTH}
      collapsedWidth={SIDER_COLLAPSED_WIDTH}
      collapsible
      collapsed={collapsed}
      onCollapse={onCollapse}
      trigger={null}
      theme="light"
    >
      <div className="zt-sider-inner">
        <div className={`zt-sider-header${collapsed ? ' zt-sider-header_collapsed' : ''}`}>
          {!collapsed && (
            <Link to={DEFAULT_APP_PATH} className="zt-sider-brand">
              <img src={Logo_src} className="zt-sider-logo" alt="logo" />
              <span className="zt-sider-brand-name">{appConfig.title}</span>
            </Link>
          )}
          <Tooltip title={collapsed ? '展开导航' : '收起导航'} placement="right">
            <Button
              type="text"
              className="zt-sider-collapse-btn"
              icon={collapsed ? <MenuUnfoldOutlined /> : <MenuFoldOutlined />}
              onClick={() => onCollapse(!collapsed)}
            />
          </Tooltip>
        </div>
        <div className="zt-sider-nav">
          <NavMenuItems
            items={navItems}
            variant="vertical"
            collapsed={collapsed}
            rotatedItemKey={rotatedItemKey}
            setRotatedItemKey={setRotatedItemKey}
          />
        </div>
        <div className="zt-sider-footer">
          <UserActions compact collapsed={collapsed} />
        </div>
      </div>
    </Sider>
  );
};

export default SideNav;
