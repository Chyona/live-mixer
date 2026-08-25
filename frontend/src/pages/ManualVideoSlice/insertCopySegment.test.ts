import { describe, expect, it } from 'vitest';
import type { SelectedCopySegment } from './types';
import { insertCopySegmentAfterNearestEnd } from './utils';

function clip(
  id: string,
  start: number,
  end: number
): SelectedCopySegment {
  return {
    id,
    speakerId: 's1',
    speakerName: '说话人 1',
    text: id,
    start,
    end,
  };
}

describe('insertCopySegmentAfterNearestEnd', () => {
  it('空列表时新片段作为第一项', () => {
    const a = clip('a', 10, 20);
    expect(insertCopySegmentAfterNearestEnd([], a)).toEqual([a]);
  });

  it('插在 end 最接近 A.start 的片段后面', () => {
    const x1 = clip('x1', 47 * 60 + 54.7, 48 * 60 + 33.2);
    const x2 = clip('x2', 48 * 60 + 33.9, 48 * 60 + 42.1);
    const a = clip('a', 48 * 60 + 42.5, 48 * 60 + 50);

    const next = insertCopySegmentAfterNearestEnd([x1, x2], a);
    expect(next.map((item) => item.id)).toEqual(['x1', 'x2', 'a']);
  });

  it('新片段落在两段之间时，跟在前一段后面', () => {
    const x1 = clip('x1', 47 * 60 + 54.7, 48 * 60 + 33.2);
    const x2 = clip('x2', 48 * 60 + 40, 48 * 60 + 50);
    const a = clip('a', 48 * 60 + 33.5, 48 * 60 + 38);

    const next = insertCopySegmentAfterNearestEnd([x1, x2], a);
    expect(next.map((item) => item.id)).toEqual(['x1', 'a', 'x2']);
  });

  it('平局时优先 end ≤ A.start 的前一段', () => {
    const before = clip('before', 0, 10);
    const after = clip('after', 20, 30);
    const a = clip('a', 15, 18);

    const next = insertCopySegmentAfterNearestEnd([before, after], a);
    expect(next.map((item) => item.id)).toEqual(['before', 'a', 'after']);
  });
});
