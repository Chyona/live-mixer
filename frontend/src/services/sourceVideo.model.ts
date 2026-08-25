export type AsrStatus = 'pending' | 'processing' | 'completed' | 'failed';

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

/** 详情接口 `asr_paragraphs` 字段 */
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
  /** 详情接口返回的 ASR 文案分段；列表接口通常不带此字段。时间单位为 ms */
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
