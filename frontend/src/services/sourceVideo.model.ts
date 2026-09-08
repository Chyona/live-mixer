export type AsrStatus = 'pending' | 'processing' | 'completed' | 'failed';

export type SourceMode = 'upcoming' | 'live' | 'replay';

export type LiveStatus =
  | 'none'
  | 'waiting'
  | 'connecting'
  | 'live'
  | 'ending'
  | 'ended'
  | 'failed';

/** ASR 词级结果，时间单位为毫秒 */
export interface LiveAsrWord {
  start_time: number;
  end_time: number;
  text: string;
}

/** ASR 段落/句段，时间单位为毫秒 */
export interface LiveAsrSegment {
  speaker: string;
  start_time: number;
  end_time: number;
  text: string;
  words?: LiveAsrWord[];
}

/** 详情接口 ASR 文案分段（asr_paragraphs / live_asr 结构相同） */
export type AsrParagraphs = LiveAsrSegment[];

/** ASR 摘要段落（时间轴默认选区），时间单位为毫秒 */
export interface AsrSummary {
  title: string;
  start_time: number;
  end_time: number;
}

export interface SourceVideo {
  id: number;
  name: string;
  live_url: string;
  m3u8_url: string;
  play_url: string;
  record_playlist_url: string;
  url_type: 'file' | 'm3u8' | string;
  source_mode: SourceMode | string;
  live_status: LiveStatus | string;
  scheduled_at: string;
  wait_deadline_at: string;
  connect_deadline_at: string;
  asr_cursor_ms: number;
  ingest_error_msg: string;
  remark: string;
  /** 时长，单位毫秒 */
  duration: number;
  ext: string;
  asr_status: AsrStatus;
  asr_progress: number;
  asr_error_msg: string;
  asr_started_at: string;
  asr_updated_at: string;
  created_at: string;
  updated_at: string;
  created_by: string;
  /** 关联的剪辑项目数量 */
  project_count: number;
  /**
   * 列表按 asr_keywords 搜索时返回的命中段落（视频文案正文）。
   * 无文案搜索时通常为空或不返回。时间单位为 ms。
   */
  matched_paragraphs?: LiveAsrSegment[] | null;
  /** 详情接口文案分段：优先 asr_paragraphs，跟播中为空时回退 live_asr；时间单位为 ms */
  asr_paragraphs?: AsrParagraphs | null;
  /** 详情接口返回的 ASR 摘要选区；无 clips0 时用于填充时间轴 */
  asr_summaries?: AsrSummary[] | null;
}

export type SourceVideoAsrFields = Pick<
  SourceVideo,
  'asr_status' | 'asr_progress' | 'asr_error_msg' | 'asr_started_at' | 'asr_updated_at'
>;

export function createInitialAsrState(): SourceVideoAsrFields {
  return {
    asr_status: 'pending',
    asr_progress: 0,
    asr_error_msg: '',
    asr_started_at: '',
    asr_updated_at: '',
  };
}

/** 添加源视频时 URL 已存在的业务码（勿放进带 http/路由依赖的 service，避免 mock 打包拉进整棵 UI） */
export const SOURCE_VIDEO_URL_DUPLICATE_CODE = 40901;

export function isSourceVideoUrlDuplicateError(payload: { code?: number }): boolean {
  return Number(payload.code) === SOURCE_VIDEO_URL_DUPLICATE_CODE;
}

export function sourceVideoPlayUrl(
  video: Pick<SourceVideo, 'play_url' | 'record_playlist_url' | 'm3u8_url' | 'live_url'>
): string {
  return (
    video.play_url?.trim() ||
    video.record_playlist_url?.trim() ||
    video.m3u8_url?.trim() ||
    video.live_url?.trim() ||
    ''
  );
}

export function isLiveIngesting(status: string | undefined): boolean {
  return status === 'waiting' || status === 'connecting' || status === 'live' || status === 'ending';
}

/** 跟播自有 EVENT 列表从开头起播，便于切片时间轴；源站滑动直播窗仍走直播边沿。 */
export function hlsStartPositionForSourceVideo(
  video: Pick<SourceVideo, 'live_status' | 'record_playlist_url'> | null | undefined
): number | undefined {
  if (!video) return undefined;
  if (video.live_status !== 'live' && video.live_status !== 'ending') return undefined;
  if (!video.record_playlist_url?.trim()) return undefined;
  return 0;
}
