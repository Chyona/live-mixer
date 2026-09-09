import type { AsrStatus, SourceVideo } from '~/services/sourceVideo';
import { isLiveIngesting } from '~/services/sourceVideo';

export const ASR_STATUS_LABEL: Record<AsrStatus, string> = {
  pending: '等待解析',
  processing: 'ASR转写中',
  completed: 'ASR已完成',
  failed: 'ASR失败',
};

export const LIVE_STATUS_LABEL: Record<string, string> = {
  none: '回放',
  waiting: '等待开播',
  connecting: '连接中',
  live: '直播中',
  ending: '合成中',
  ended: '已关播',
  failed: '跟播失败',
};

const LIVE_WINDOW_ASR_STATUSES = new Set(['live', 'ending', 'ended']);

export function isAsrReady(status: AsrStatus): boolean {
  return status === 'completed';
}

/** 跟播窗口 ASR 已写出可成片区间（与后端 ASRCoversClips 对齐）。 */
export function hasLiveWindowAsrCoverage(
  video: Pick<SourceVideo, 'live_status' | 'asr_cursor_ms'>
): boolean {
  if (!LIVE_WINDOW_ASR_STATUSES.has(String(video.live_status))) return false;
  return Number(video.asr_cursor_ms) > 0;
}

/** 跟播中 ASR 未完成时，可选区结束时间（秒）；完成态或不适用则无限制。 */
export function getAsrSelectableEndSec(
  video: Pick<SourceVideo, 'asr_status' | 'live_status' | 'asr_cursor_ms'> | null | undefined
): number | null {
  if (!video || isAsrReady(video.asr_status)) return null;
  if (!hasLiveWindowAsrCoverage(video)) return null;
  const cursorSec = Number(video.asr_cursor_ms) / 1000;
  if (!Number.isFinite(cursorSec) || cursorSec <= 0) return null;
  return cursorSec;
}

export function shouldPollLiveAsrProgress(
  video: Pick<SourceVideo, 'live_status' | 'asr_status'> | null | undefined
): boolean {
  if (!video) return false;
  if (isLiveIngesting(video.live_status)) return true;
  return (
    video.live_status === 'ended' &&
    video.asr_status !== 'completed' &&
    video.asr_status !== 'failed'
  );
}

export function getAsrActionDisabledReason(
  video: Pick<SourceVideo, 'asr_status' | 'asr_error_msg' | 'live_status' | 'asr_cursor_ms' | 'ingest_error_msg'>
): string | null {
  if (isAsrReady(video.asr_status)) return null;

  if (video.live_status === 'failed') {
    const msg = video.ingest_error_msg?.trim() || video.asr_error_msg?.trim();
    return msg ? `跟播失败：${msg}` : '跟播失败，暂无法进行此操作';
  }

  if (video.live_status === 'waiting' || video.live_status === 'connecting') {
    return '直播尚未开始，暂无法进行此操作';
  }

  if (hasLiveWindowAsrCoverage(video)) {
    return null;
  }

  const resolvedMessage = video.asr_error_msg?.trim();
  if (video.asr_status === 'failed') {
    return resolvedMessage ? `ASR 解析失败：${resolvedMessage}` : 'ASR 解析失败，暂无法进行此操作';
  }

  return `${ASR_STATUS_LABEL[video.asr_status]}，暂无法进行此操作`;
}
