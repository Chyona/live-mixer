import { describe, expect, it } from 'vitest';
import { resolveSliceTimelineDurationSec } from './duration';

describe('resolveSliceTimelineDurationSec', () => {
  it('跟播在播放器时长为 Infinity 时使用后端已录时长', () => {
    expect(
      resolveSliceTimelineDurationSec({
        playerDurationSec: Number.POSITIVE_INFINITY,
        backendDurationMs: 18 * 60 * 1000,
        liveIngesting: true,
      })
    ).toBe(18 * 60);
  });

  it('跟播取播放器与后端中的较大值，便于时间轴随录像增长', () => {
    expect(
      resolveSliceTimelineDurationSec({
        playerDurationSec: 12,
        backendDurationMs: 18000,
        asrCursorMs: 15000,
        liveIngesting: true,
      })
    ).toBe(18);
  });

  it('回放优先使用播放器有限时长', () => {
    expect(
      resolveSliceTimelineDurationSec({
        playerDurationSec: 90.5,
        backendDurationMs: 80000,
        liveIngesting: false,
      })
    ).toBe(90.5);
  });
});
