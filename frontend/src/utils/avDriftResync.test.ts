import { describe, expect, it } from 'vitest';
import {
  AV_DRIFT_THRESHOLD_SEC,
  detectPlaybackFreeze,
  shouldCorrectAvDrift,
} from './avDriftResync';

describe('shouldCorrectAvDrift', () => {
  it('returns false within threshold', () => {
    expect(shouldCorrectAvDrift(0.1, 0.35)).toBe(false);
    expect(shouldCorrectAvDrift(-0.2, 0.35)).toBe(false);
  });

  it('returns true at or beyond threshold', () => {
    expect(shouldCorrectAvDrift(0.35, 0.35)).toBe(true);
    expect(shouldCorrectAvDrift(-0.5, 0.35)).toBe(true);
  });

  it('uses default threshold', () => {
    expect(shouldCorrectAvDrift(AV_DRIFT_THRESHOLD_SEC)).toBe(true);
    expect(shouldCorrectAvDrift(AV_DRIFT_THRESHOLD_SEC - 0.01)).toBe(false);
  });

  it('rejects non-finite drift', () => {
    expect(shouldCorrectAvDrift(Number.NaN)).toBe(false);
    expect(shouldCorrectAvDrift(Number.POSITIVE_INFINITY)).toBe(false);
  });
});

describe('detectPlaybackFreeze', () => {
  it('detects stall while wall clock advances', () => {
    expect(
      detectPlaybackFreeze({
        paused: false,
        seeking: false,
        wallDeltaSec: 0.4,
        mediaDeltaSec: 0.01,
      })
    ).toBe(true);
  });

  it('ignores pause, seek, or normal playback', () => {
    expect(
      detectPlaybackFreeze({
        paused: true,
        seeking: false,
        wallDeltaSec: 0.4,
        mediaDeltaSec: 0,
      })
    ).toBe(false);
    expect(
      detectPlaybackFreeze({
        paused: false,
        seeking: true,
        wallDeltaSec: 0.4,
        mediaDeltaSec: 0,
      })
    ).toBe(false);
    expect(
      detectPlaybackFreeze({
        paused: false,
        seeking: false,
        wallDeltaSec: 0.4,
        mediaDeltaSec: 0.2,
      })
    ).toBe(false);
  });
});
