import { useState, useEffect, useCallback } from 'react';
import { debounce } from 'lodash-es';
import { ScreenBreakpoint } from '~/utils/const';

export const useResponsive = (breakpoint = ScreenBreakpoint) => {
  const [isMobile, setIsMobile] = useState(false);

  const checkMobile = useCallback(() => {
    if (typeof window === 'undefined') return false;

    const isMobileView = window.innerWidth < breakpoint;
    setIsMobile(isMobileView);
    return isMobileView;
  }, [breakpoint]);

  useEffect(() => {
    const handleResize = debounce(checkMobile, 150);
    handleResize();
    window.addEventListener('resize', handleResize);
    return () => {
      window.removeEventListener('resize', handleResize);
      handleResize.cancel?.();
    };
  }, [checkMobile]);

  return {
    isMobile,
    isDesktop: !isMobile,
    isPhone: isMobile,
    breakpoint,
  };
};
