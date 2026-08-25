import { Avatar, Button, Divider, Dropdown, Flex, Tooltip, Typography } from 'antd';
import { FaSignOutAlt, FaUser } from 'react-icons/fa';
import { useAuth } from '~/context/AuthContext';
import { hasAuthCredentials, type UserLoginResult } from '~/services/login';
import { openLogin } from '~/utils/loginFlow';

type UserActionsProps = {
  compact?: boolean;
  collapsed?: boolean;
};

const userDropdownMenu = (userInfo: Partial<UserLoginResult>, logout: () => void) => ({
  className: 'user-dropdown-menu',
  items: [
    {
      key: 'user-info',
      label: (
        <>
          <div className="user-dropdown-info">
            <div className="user-dropdown-avatar">
              <Avatar size={48} className="user-dropdown-avatar-icon" icon={<FaUser size={22} />} />
            </div>
            <div className="user-dropdown-details">
              <div className="user-dropdown-name">{userInfo.username || '未登录'}</div>
              <div className="user-dropdown-id">
                ID: {userInfo.id || '-'}
                <Typography.Text
                  copyable={{ text: String(userInfo.id || '-') }}
                  className="ml-1 packages-user-id-copy"
                />
              </div>
            </div>
          </div>
          <Divider className="user-dropdown-divider" />
          <Flex className="user-dropdown-logout">
            <Button color="default"
              variant="filled" className="user-dropdown-logout-btn" block onClick={() => logout()}>
              <FaSignOutAlt size={18} />
              <span>退出登录</span>
            </Button>
          </Flex>
        </>
      ),
      disabled: true,
    },
  ],
});

const UserActions = ({ compact, collapsed = false }: UserActionsProps) => {
  const { userInfo = {}, logout } = useAuth();

  if (hasAuthCredentials(userInfo)) {
    if (compact) {
      const userPanel = (
        <div
          className={`zt-sider-user-panel ${collapsed ? 'zt-sider-user-panel_collapsed' : 'zt-sider-user-panel_expanded'}`}
        >
          <Avatar size={40} className="zt-sider-user-avatar" icon={<FaUser size={18} />} />
          {!collapsed && (
            <Flex vertical align="flex-start" className="zt-sider-user-meta">
              <span className="zt-sider-user-name">{userInfo.username || '未登录'}</span>
              <span className="zt-sider-user-id">
                ID: {userInfo.id || '-'}
                <Typography.Text
                  copyable={{ text: userInfo.id || '-' }}
                  className="zt-sider-user-id-copy"
                  onClick={(e) => e.stopPropagation()}
                />
              </span>
            </Flex>
          )}
        </div>
      );

      return (
        <Dropdown menu={userDropdownMenu(userInfo, logout)} trigger={['click']}>
          {collapsed ? (
            <Tooltip title={userInfo.username || '用户'} placement="right">
              {userPanel}
            </Tooltip>
          ) : (
            userPanel
          )}
        </Dropdown>
      );
    }

    return (
      <Dropdown menu={userDropdownMenu(userInfo, logout)}>
        <Flex className="nav-user-avatar" align="center">
          {userInfo.username?.slice(0, 1)}
        </Flex>
      </Dropdown>
    );
  }

  const loginButton = (
    <Button
      type="primary"
      className={`login-btn ${compact ? 'login-btn_compact' : ''}`}
      block={compact && !collapsed}
      icon={collapsed ? <FaUser size={16} /> : undefined}
      onClick={() => openLogin()}
    >
      {collapsed ? null : '登录'}
    </Button>
  );

  if (collapsed) {
    return (
      <Tooltip title="登录" placement="right">
        {loginButton}
      </Tooltip>
    );
  }

  return loginButton;
};

export default UserActions;
