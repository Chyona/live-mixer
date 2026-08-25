import { Flex } from 'antd';
import { FaBars } from 'react-icons/fa';
import { Link, useLocation } from 'react-router-dom';
import { useMemo, useState } from 'react';
import { RoutesCfg } from '~/routes/const';
import { useResponsive } from '~/hooks/useResponsive';
import NavMenuItems from './NavMenuItems';
import UserActions from './UserActions';
import { getNavActive, useNavItems } from './useNavItems';
import type { SliceEditorEntryFrom } from '~/routes/links';

import './index.css';

const mobileNavPaths = ['/source-videos', '/slices', '/tasks'];

export const MobileBottomNav = () => {
  const location = useLocation();
  const entryFrom = (location.state as { from?: SliceEditorEntryFrom } | null)?.from;

  const navItems = useMemo(() => {
    return RoutesCfg.filter((item) => mobileNavPaths.includes(item.path));
  }, []);

  return (
    <div className="mobile-bottom-nav">
      {navItems.map((item) => {
        const isActive = getNavActive(item.path, location.pathname, entryFrom);

        if (item.links) {
          return (
            <div
              key={item.path}
              className={`mobile-nav-item ${isActive ? 'active' : ''}`}
              onClick={() => {
                const firstLink = item.links?.[0];
                if (firstLink) {
                  window.open(firstLink.href, '_blank', 'noopener,noreferrer');
                }
              }}
            >
              <item.icon size={22} className={isActive ? 'nav-icon-active' : undefined} />
              <span>{item.text}</span>
            </div>
          );
        }

        if (item.href) {
          return (
            <a
              key={item.path}
              className="mobile-nav-item"
              href={item.href}
              target="_blank"
              rel="noopener noreferrer"
            >
              <item.icon size={22} />
              <span>{item.text}</span>
            </a>
          );
        }

        return (
          <Link
            key={item.path}
            to={item.path}
            className={`mobile-nav-item ${isActive ? 'active' : ''}`}
          >
            <item.icon size={22} className={isActive ? 'nav-icon-active' : undefined} />
            <span>{item.text}</span>
          </Link>
        );
      })}
    </div>
  );
};

const TopNav = () => {
  const { isMobile } = useResponsive();
  const { navItems, rotatedItemKey, setRotatedItemKey } = useNavItems();
  const [showMenu, setShowMenu] = useState(false);

  return (
    <Flex className="flex-1 zt-nav-ct" justify="space-between" align="center" gap={16}>
      {isMobile ? (
        <>
          <div className="custom-toggler mr-2" onClick={() => setShowMenu(!showMenu)}>
            <FaBars size={32} />
          </div>
          {showMenu && (
            <Flex className="zt-nav zt-nav_expand" vertical>
              <NavMenuItems
                items={navItems}
                variant="vertical"
                rotatedItemKey={rotatedItemKey}
                setRotatedItemKey={setRotatedItemKey}
                showMenu={showMenu}
                setShowMenu={setShowMenu}
              />
            </Flex>
          )}
        </>
      ) : (
        <NavMenuItems
          items={navItems}
          variant="horizontal"
          rotatedItemKey={rotatedItemKey}
          setRotatedItemKey={setRotatedItemKey}
        />
      )}
      <UserActions />
    </Flex>
  );
};

export { default as SideNav } from './SideNav';
export default TopNav;
