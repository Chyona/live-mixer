import { useEffect } from 'react';
import { useLocation } from 'react-router-dom';
import { appConfig } from '~/utils/config';
import { trackEvent } from '~/utils/gtm';

export default function GtmRouterTracker() {
  const location = useLocation();

  useEffect(() => {
    if (!appConfig.enableGtm) return;
    trackEvent('pageview', {
      page_path: location.pathname + location.search,
    });
  }, [location]);

  return null;
}
