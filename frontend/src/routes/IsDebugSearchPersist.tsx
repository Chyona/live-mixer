import { useEffect, useRef } from 'react';
import { useLocation, useNavigate, useNavigationType } from 'react-router-dom';
import {
  isDebugSearchPersistEnabled,
  syncDebugSearchPersistFromUrl,
} from '~/utils/asrParagraphsKey';

/** 路由变化时保留 ?isdebug=true，直至 URL 显式 isdebug=false 或浏览器 POP 去掉该参数 */
export default function IsDebugSearchPersist() {
  const location = useLocation();
  const navigate = useNavigate();
  const navigationType = useNavigationType();
  const prevSearchRef = useRef(location.search);

  useEffect(() => {
    try {
      syncDebugSearchPersistFromUrl(location.search);

      const params = new URLSearchParams(location.search);
      const isdebug = params.get('isdebug')?.toLowerCase();

      if (isdebug === 'false') {
        prevSearchRef.current = location.search;
        return;
      }

      const prevHadIsdebug = prevSearchRef.current.includes('isdebug=true');
      const hasIsdebug = isdebug === 'true';

      if (prevHadIsdebug && !hasIsdebug && navigationType === 'POP') {
        syncDebugSearchPersistFromUrl('?isdebug=false');
        prevSearchRef.current = location.search;
        return;
      }

      if (isDebugSearchPersistEnabled() && !params.has('isdebug')) {
        params.set('isdebug', 'true');
        navigate(
          {
            pathname: location.pathname,
            search: `?${params.toString()}`,
            hash: location.hash,
          },
          { replace: true, state: location.state }
        );
        prevSearchRef.current = `?${params.toString()}`;
        return;
      }

      prevSearchRef.current = location.search;
    } catch (error) {
      console.warn('[isdebug] 路由参数跟随异常，已跳过', error);
    }
  }, [
    location.hash,
    location.pathname,
    location.search,
    location.state,
    navigate,
    navigationType,
  ]);

  return null;
}
