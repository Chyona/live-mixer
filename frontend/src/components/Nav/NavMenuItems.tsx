import { Dropdown, Tooltip } from 'antd';
import type { ReactNode } from 'react';
import { FaCaretDown } from 'react-icons/fa';
import { Link } from 'react-router-dom';
import { type RouteCfgType } from '~/routes/const';

type NavMenuItemsProps = {
  items: RouteCfgType[];
  variant?: 'horizontal' | 'vertical';
  collapsed?: boolean;
  rotatedItemKey?: string | null;
  setRotatedItemKey?: (key: string | null) => void;
  showMenu?: boolean;
  setShowMenu?: (show: boolean) => void;
};

const NavMenuItems = ({
  items,
  variant = 'horizontal',
  collapsed = false,
  rotatedItemKey = null,
  setRotatedItemKey = () => {},
  showMenu = false,
  setShowMenu = () => {},
}: NavMenuItemsProps) => {
  const isVertical = variant === 'vertical';
  const navClassName = [
    'zt-nav',
    isVertical ? 'zt-nav_vertical' : '',
    collapsed ? 'zt-nav_collapsed' : '',
  ]
    .filter(Boolean)
    .join(' ');

  const renderIconText = (item: RouteCfgType) => (
    <>
      <span className="nav-item-icon">
        <item.icon size={20} {...item.iconProps} />
      </span>
      <span className="nav-item-text">{item.text}</span>
      {item.links && !collapsed && (
        <FaCaretDown
          size={14}
          className={`ml-1 ${rotatedItemKey === item.path ? 'rotate-icon' : ''}`}
        />
      )}
    </>
  );

  const wrapCollapsedTooltip = (item: RouteCfgType, node: ReactNode) => {
    if (!collapsed) return node;
    return (
      <Tooltip key={item.path} title={item.text} placement="right">
        {node}
      </Tooltip>
    );
  };

  const renderMenuItem = (item: RouteCfgType) => {
    if (item.hideInMenu) return null;

    if (item.links) {
      const menuProps = {
        className: 'nav-dropdown-menu',
        items: item.links.map((link, linkIndex) => ({
          key: linkIndex,
          label: (
            <a
              href={link.href}
              target="_blank"
              rel="noopener noreferrer"
              className="flex items-center gap-2"
              onClick={() => setShowMenu(!showMenu)}
            >
              <link.icon size={18} />
              {link.text}
            </a>
          ),
        })),
        onMouseEnter: () => setRotatedItemKey(item.path),
        onMouseLeave: () => setRotatedItemKey(null),
      };

      return wrapCollapsedTooltip(
        item,
        <Dropdown
          key={item.path}
          menu={menuProps}
          placement={isVertical ? 'bottomRight' : 'bottom'}
        >
          <a
            className={`nav-item ${rotatedItemKey === item.path ? 'hover' : ''}`}
            onClick={(e) => e.preventDefault()}
            onMouseEnter={() => setRotatedItemKey(item.path)}
            onMouseLeave={() => setRotatedItemKey(null)}
          >
            {renderIconText(item)}
          </a>
        </Dropdown>
      );
    }

    if (item.href) {
      return wrapCollapsedTooltip(
        item,
        <a
          className="nav-item"
          href={item.href}
          target="_blank"
          rel="noopener noreferrer"
          key={item.path}
          onClick={() => setShowMenu(!showMenu)}
        >
          {renderIconText(item)}
        </a>
      );
    }

    return wrapCollapsedTooltip(
      item,
      <Link
        key={item.path}
        to={item.path}
        onClick={() => setShowMenu(!showMenu)}
        className={`nav-item ${item.active ? 'active' : ''}`}
      >
        {renderIconText(item)}
      </Link>
    );
  };

  const visibleItems = items.filter((item) => !item.hideInMenu);

  return (
    <div className={navClassName}>
      {visibleItems.map((item, index) => {
        const prevItem = visibleItems[index - 1];
        const showGroupDivider =
          isVertical &&
          !collapsed &&
          item.group &&
          index > 0 &&
          prevItem?.group !== item.group;
        const showGroupTitle =
          isVertical && !collapsed && item.group && prevItem?.group !== item.group;

        return (
          <div key={item.path} className="zt-nav-entry">
            {showGroupDivider && <div className="zt-nav-divider" />}
            {showGroupTitle && <div className="zt-nav-group-title">{item.group}</div>}
            {renderMenuItem(item)}
          </div>
        );
      })}
    </div>
  );
};

export default NavMenuItems;
