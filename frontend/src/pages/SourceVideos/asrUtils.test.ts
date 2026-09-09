import { describe, expect, it } from 'vitest';
import {
  getAsrActionDisabledReason,
  getAsrSelectableEndSec,
  hasLiveWindowAsrCoverage,
  shouldPollLiveAsrProgress,
} from './asrUtils';

const liveProcessing = {
  asr_status: 'processing' as const,
  asr_error_msg: '',
  ingest_error_msg: '',
  live_status: 'live',
  asr_cursor_ms: 47 * 60 * 1000 + 32 * 1000,
};

describe('hasLiveWindowAsrCoverage', () => {
  it('跟播中且 cursor 大于 0 时允许按已解析区间操作', () => {
    expect(hasLiveWindowAsrCoverage(liveProcessing)).toBe(true);
  });

  it('关播后 ASR 未完成但已有窗口转写时仍视为可覆盖', () => {
    expect(hasLiveWindowAsrCoverage({ live_status: 'ended', asr_cursor_ms: 1000 })).toBe(true);
  });

  it('回放或尚无 cursor 时不可按窗口覆盖', () => {
    expect(hasLiveWindowAsrCoverage({ live_status: 'none', asr_cursor_ms: 8000 })).toBe(false);
    expect(hasLiveWindowAsrCoverage({ live_status: 'live', asr_cursor_ms: 0 })).toBe(false);
  });
});

describe('getAsrSelectableEndSec', () => {
  it('ASR 完成后不限制选区', () => {
    expect(
      getAsrSelectableEndSec({
        ...liveProcessing,
        asr_status: 'completed',
      })
    ).toBeNull();
  });

  it('跟播窗口 ASR 返回 cursor 秒数', () => {
    expect(getAsrSelectableEndSec(liveProcessing)).toBe(47 * 60 + 32);
  });
});

describe('getAsrActionDisabledReason', () => {
  it('跟播已有窗口 ASR 时允许进入切片', () => {
    expect(getAsrActionDisabledReason(liveProcessing)).toBeNull();
  });

  it('已关播但窗口 ASR 未写完时仍允许进入切片', () => {
    expect(
      getAsrActionDisabledReason({
        ...liveProcessing,
        live_status: 'ended',
      })
    ).toBeNull();
  });

  it('回放 ASR 进行中时禁止进入', () => {
    expect(
      getAsrActionDisabledReason({
        asr_status: 'processing',
        asr_error_msg: '',
        ingest_error_msg: '',
        live_status: 'none',
        asr_cursor_ms: 0,
      })
    ).toBe('ASR转写中，暂无法进行此操作');
  });
});

describe('shouldPollLiveAsrProgress', () => {
  it('跟播中持续轮询', () => {
    expect(shouldPollLiveAsrProgress({ live_status: 'live', asr_status: 'processing' })).toBe(true);
  });

  it('关播后 ASR 未完成仍轮询，便于 cursor 追平', () => {
    expect(shouldPollLiveAsrProgress({ live_status: 'ended', asr_status: 'processing' })).toBe(true);
    expect(shouldPollLiveAsrProgress({ live_status: 'ended', asr_status: 'completed' })).toBe(false);
  });
});
