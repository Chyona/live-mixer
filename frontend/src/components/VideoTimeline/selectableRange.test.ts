import { describe, expect, it } from 'vitest';
import { clampRangeToSelectableEnd, resolveSelectableMaxEnd } from './selectableRange';

describe('clampRangeToSelectableEnd', () => {
  it('无上限时原样返回', () => {
    expect(clampRangeToSelectableEnd(1, 10)).toEqual({ start: 1, end: 10, clamped: false });
  });

  it('选区越过已解析终点时截断', () => {
    expect(clampRangeToSelectableEnd(0, 3312, 2852)).toEqual({
      start: 0,
      end: 2852,
      clamped: true,
    });
  });

  it('整段落在未解析区间时返回 null', () => {
    expect(clampRangeToSelectableEnd(3000, 3312, 2852)).toBeNull();
  });
});

describe('resolveSelectableMaxEnd', () => {
  it('取 duration 与 cursor 的较小值', () => {
    expect(resolveSelectableMaxEnd(3312, 2852)).toBe(2852);
    expect(resolveSelectableMaxEnd(2000, 2852)).toBe(2000);
    expect(resolveSelectableMaxEnd(3312)).toBe(3312);
  });
});
