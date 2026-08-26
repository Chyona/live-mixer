import type { MouseEvent } from 'react';

export function shouldGuardNavTarget(
  target: string,
  pathname: string,
  search: string
): boolean {
  const current = `${pathname}${search}`;
  return target !== current;
}

export function handleGuardedNavClick(
  event: MouseEvent,
  target: string,
  pathname: string,
  search: string,
  requestNavigate: ((to: string) => void) | undefined,
  onAfterClick?: () => void
) {
  onAfterClick?.();
  if (!requestNavigate || !shouldGuardNavTarget(target, pathname, search)) {
    return;
  }
  event.preventDefault();
  requestNavigate(target);
}
