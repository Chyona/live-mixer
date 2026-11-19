import { describe, expect, it, vi } from 'vitest';
import { resolveSeekTarget, seekHtmlVideo } from './seekMedia';

describe('resolveSeekTarget', () => {
  it('rejects non-finite time', () => {
    expect(resolveSeekTarget(Number.NaN, 120)).toBeNull();
    expect(resolveSeekTarget(Number.POSITIVE_INFINITY, 120)).toBeNull();
  });

  it('clamps negative time to 0', () => {
    expect(resolveSeekTarget(-3, 120)).toBe(0);
  });

  it('keeps time when duration is unknown', () => {
    expect(resolveSeekTarget(14.37, Number.NaN)).toBe(14.37);
    expect(resolveSeekTarget(14.37, 0)).toBe(14.37);
  });

  it('clamps to just before duration', () => {
    expect(resolveSeekTarget(120, 120)).toBeCloseTo(119.96, 5);
    expect(resolveSeekTarget(200, 120)).toBeCloseTo(119.96, 5);
  });
});

describe('seekHtmlVideo', () => {
  it('assigns currentTime and plays when paused at ended', async () => {
    const play = vi.fn().mockResolvedValue(undefined);
    const video = {
      duration: 1200,
      currentTime: 1200,
      paused: true,
      ended: true,
      play,
    } as unknown as HTMLVideoElement;

    const target = seekHtmlVideo(video, 877.6);

    expect(target).toBe(877.6);
    expect(video.currentTime).toBe(877.6);
    expect(play).toHaveBeenCalledTimes(1);

    await Promise.resolve();
  });

  it('does not call play when already playing', () => {
    const play = vi.fn().mockResolvedValue(undefined);
    const video = {
      duration: 1200,
      currentTime: 10,
      paused: false,
      ended: false,
      play,
    } as unknown as HTMLVideoElement;

    seekHtmlVideo(video, 40);
    expect(video.currentTime).toBe(40);
    expect(play).not.toHaveBeenCalled();
  });

  it('retries currentTime after play if ended seek was ignored', async () => {
    let current = 1200;
    let ended = true;
    const video = {
      duration: 1200,
      paused: true,
      get ended() {
        return ended;
      },
      get currentTime() {
        return current;
      },
      set currentTime(value: number) {
        if (ended) return;
        current = value;
      },
      play: vi.fn().mockImplementation(() => {
        ended = false;
        return Promise.resolve();
      }),
    } as unknown as HTMLVideoElement;

    seekHtmlVideo(video, 100);
    expect(video.currentTime).toBe(1200);

    await Promise.resolve();
    expect(video.currentTime).toBe(100);
  });
});
