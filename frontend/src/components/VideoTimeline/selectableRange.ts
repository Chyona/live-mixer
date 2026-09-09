export const SELECTABLE_RANGE_EPSILON = 0.05;

export function clampRangeToSelectableEnd(
  start: number,
  end: number,
  selectableEnd?: number
): { start: number; end: number; clamped: boolean } | null {
  const lo = Math.min(start, end);
  const hi = Math.max(start, end);
  if (selectableEnd == null || !Number.isFinite(selectableEnd) || selectableEnd <= 0) {
    return { start: lo, end: hi, clamped: false };
  }
  if (lo >= selectableEnd - SELECTABLE_RANGE_EPSILON) {
    return null;
  }
  const nextEnd = Math.min(hi, selectableEnd);
  return {
    start: lo,
    end: nextEnd,
    clamped: hi > selectableEnd + SELECTABLE_RANGE_EPSILON,
  };
}

export function resolveSelectableMaxEnd(duration: number, selectableEnd?: number): number {
  if (selectableEnd == null || !Number.isFinite(selectableEnd) || selectableEnd <= 0) {
    return duration;
  }
  return Math.max(0, Math.min(duration, selectableEnd));
}
