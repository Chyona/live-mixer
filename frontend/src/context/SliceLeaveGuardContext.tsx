import {
  createContext,
  useCallback,
  useContext,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  type ReactNode,
} from 'react';
import { Button, Space } from 'antd';
import { useLocation, useNavigate } from 'react-router-dom';
import { toast } from '~/utils/toast';

type LeaveGuard = {
  isDirty: () => boolean;
};

type SliceLeaveGuardContextValue = {
  register: (guard: LeaveGuard | null) => void;
  confirmLeave: (action: () => void) => void;
  requestNavigate: (to: string) => void;
};

const SliceLeaveGuardContext = createContext<SliceLeaveGuardContextValue | null>(null);

const LEAVE_CONFIRM_KEY = 'slice-leave-confirm';
const LEAVE_CONFIRM_TITLE = '确认离开当前项目？';
const LEAVE_CONFIRM_CONTENT = '有未保存的编辑内容，离开后将丢失，是否继续？';

function openLeaveConfirmToast(onLeave: () => void) {
  toast.notify.warning(LEAVE_CONFIRM_TITLE, LEAVE_CONFIRM_CONTENT, {
    key: LEAVE_CONFIRM_KEY,
    duration: 0,
    btn: (
      <Space size={8}>
        <Button size="small" onClick={() => toast.notify.destroy(LEAVE_CONFIRM_KEY)}>
          留在此页
        </Button>
        <Button
          size="small"
          type="primary"
          danger
          onClick={() => {
            toast.notify.destroy(LEAVE_CONFIRM_KEY);
            onLeave();
          }}
        >
          离开
        </Button>
      </Space>
    ),
  });
}

export function SliceLeaveGuardProvider({ children }: { children: ReactNode }) {
  const guardRef = useRef<LeaveGuard | null>(null);
  const navigate = useNavigate();
  const location = useLocation();

  const register = useCallback((guard: LeaveGuard | null) => {
    guardRef.current = guard;
  }, []);

  const confirmLeave = useCallback((action: () => void) => {
    const guard = guardRef.current;
    if (!guard?.isDirty()) {
      action();
      return;
    }

    openLeaveConfirmToast(() => {
      guardRef.current = null;
      action();
    });
  }, []);

  const requestNavigate = useCallback(
    (to: string) => {
      const current = `${location.pathname}${location.search}`;
      if (to === current) return;
      confirmLeave(() => navigate(to));
    },
    [confirmLeave, location.pathname, location.search, navigate]
  );

  const value = useMemo(
    () => ({ register, confirmLeave, requestNavigate }),
    [register, confirmLeave, requestNavigate]
  );

  return (
    <SliceLeaveGuardContext.Provider value={value}>{children}</SliceLeaveGuardContext.Provider>
  );
}

export function useSliceLeaveGuardContext() {
  return useContext(SliceLeaveGuardContext);
}

export function useSliceProjectLeaveGuard(getIsDirty: () => boolean) {
  const ctx = useSliceLeaveGuardContext();
  const getIsDirtyRef = useRef(getIsDirty);
  getIsDirtyRef.current = getIsDirty;

  useLayoutEffect(() => {
    if (!ctx) return;
    ctx.register({ isDirty: () => getIsDirtyRef.current() });
    return () => ctx.register(null);
  }, [ctx]);

  useEffect(() => {
    const onBeforeUnload = (event: BeforeUnloadEvent) => {
      if (!getIsDirtyRef.current()) return;
      event.preventDefault();
      event.returnValue = '';
    };

    window.addEventListener('beforeunload', onBeforeUnload);
    return () => window.removeEventListener('beforeunload', onBeforeUnload);
  }, []);

  const confirmLeave = useCallback(
    (action: () => void) => {
      if (ctx) {
        ctx.confirmLeave(action);
        return;
      }
      action();
    },
    [ctx]
  );

  return { confirmLeave, requestNavigate: ctx?.requestNavigate };
}
