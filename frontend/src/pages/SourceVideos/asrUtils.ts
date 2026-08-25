import type { AsrStatus } from '~/services/sourceVideo';

export const ASR_STATUS_LABEL: Record<AsrStatus, string> = {
  pending: '等待解析',
  processing: 'ASR转写中',
  completed: 'ASR已完成',
  failed: 'ASR失败',
};

export function isAsrReady(status: AsrStatus): boolean {
  return status === 'completed';
}

export function getAsrActionDisabledReason(status: AsrStatus, asr_error_msg?: string): string | null {
  const resolvedMessage = asr_error_msg?.trim();

  if (isAsrReady(status)) return null;

  if (status === 'failed') {
    return resolvedMessage ? `ASR 解析失败：${resolvedMessage}` : 'ASR 解析失败，暂无法进行此操作';
  }

  return `${ASR_STATUS_LABEL[status]}，暂无法进行此操作`;
}
