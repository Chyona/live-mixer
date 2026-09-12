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

/** 跟播离散媒体窗（与后端 media_windows jsonb 对齐）；预览/ASR 用前 N 窗拼接的 master（live_url） */
export interface MediaWindowMeta {
  i: number;
  start_ms: number;
  end_ms: number;
  dur_ms: number;
  url?: string;
  ts_url?: string;
  ready: boolean;
}

/** 跟播音画/字幕对齐诊断摘要（详情 API align_diag） */
export interface AlignDiagSnapshot {
  play_url: string;
  live_url: string;
  same_play_and_live: boolean;
  master_ready_ms: number;
  duration_ms: number;
  window_count: number;
  asr_cursor_ms: number;
  asr_progress: number;
  live_status: string;
  hint: string;
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
  /** 跟播离散媒体窗；拼接主片见 live_url */
  media_windows?: MediaWindowMeta[];
  /**
   * 列表按 asr_keywords 搜索时返回的命中段落（视频文案正文）。
   * 无文案搜索时通常为空或不返回。时间单位为 ms。
   */
  matched_paragraphs?: LiveAsrSegment[] | null;
  /** 详情接口文案分段：优先 asr_paragraphs，跟播中为空时回退 live_asr；时间单位为 ms */
  asr_paragraphs?: AsrParagraphs | null;
  /** 详情接口返回的 ASR 摘要选区；无 clips0 时用于填充时间轴 */
  asr_summaries?: AsrSummary[] | null;
  /** 跟播对齐诊断摘要（仅详情接口） */
  align_diag?: AlignDiagSnapshot | null;
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

function isProbablyM3u8Url(url: string): boolean {
  return /\.m3u8(\?|#|$)/i.test(url.trim());
}

function readyMediaWindows(windows: MediaWindowMeta[] | undefined): MediaWindowMeta[] {
  if (!windows?.length) return [];
  return windows.filter((w) => w.ready && Boolean(w.url?.trim() || w.ts_url?.trim()));
}

function firstReadyWindowMp4Url(windows: MediaWindowMeta[] | undefined): string {
  const ready = readyMediaWindows(windows);
  if (ready.length !== 1) return '';
  return ready[0]?.url?.trim() || '';
}

function isLiveIngestStatus(status: string | undefined): boolean {
  return (
    status === 'waiting' ||
    status === 'connecting' ||
    status === 'live' ||
    status === 'ending' ||
    status === 'ended'
  );
}

/**
 * 切片页预览地址：跟播必须与前 N 窗拼接主 MP4（live_url）/ ASR / 一键成片同一时间轴。
 * 禁止回退到源站滑动 m3u8。
 * - 优先 live_url（非 m3u8 的拼接 master）
 * - 仅一窗就绪且尚无 master 时，可暂用该窗 MP4
 * - >=2 窗时必须使用 master，不得用单窗或 HLS
 */
export function sourceVideoPlayUrl(
  video: Pick<
    SourceVideo,
    'play_url' | 'record_playlist_url' | 'm3u8_url' | 'live_url' | 'live_status'
  > & { media_windows?: MediaWindowMeta[] }
): string {
  const status = String(video.live_status ?? 'none');
  const backendPlay = video.play_url?.trim() || '';
  const live = video.live_url?.trim() || '';
  const remote = video.m3u8_url?.trim() || '';
  const windows = video.media_windows;
  const ready = readyMediaWindows(windows);

  if (isLiveIngestStatus(status)) {
    if (live && !isProbablyM3u8Url(live) && (ready.length > 0 || status === 'ended')) {
      return live;
    }
    if (ready.length === 1) {
      const win = firstReadyWindowMp4Url(windows);
      if (win) return win;
    }
    if (
      backendPlay &&
      backendPlay !== remote &&
      !isProbablyM3u8Url(backendPlay) &&
      (ready.length > 0 || status === 'ended')
    ) {
      return backendPlay;
    }
    return '';
  }

  return backendPlay || live || remote || '';
}

export function isLiveIngesting(status: string | undefined): boolean {
  return status === 'waiting' || status === 'connecting' || status === 'live' || status === 'ending';
}

/** 跟播预览为递增 MP4，无需 HLS startPosition。 */
export function hlsStartPositionForSourceVideo(
  _video: Pick<SourceVideo, 'live_status' | 'record_playlist_url'> | null | undefined
): number | undefined {
  return undefined;
}
