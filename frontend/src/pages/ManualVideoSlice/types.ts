export interface TranscriptWord {
  /** 秒 */
  start: number;
  /** 秒 */
  end: number;
  text: string;
}

export interface TranscriptSegment {
  id: string;
  start: number;
  end: number;
  text: string;
  /** 字级时间轴（来自 asr_paragraphs.words），可选 */
  words?: TranscriptWord[];
}

export interface TranscriptParagraph {
  id: string;
  speakerId: string;
  speakerName: string;
  segments: TranscriptSegment[];
}

export interface SelectedCopySegment {
  id: string;
  speakerId: string;
  speakerName: string;
  text: string;
  start: number;
  end: number;
  /** 选入时的原始起点；用于计算前方留白，并限制收回时不切入原文案 */
  originStart?: number;
  /** 选入时的原始终点；用于计算后方留白，并限制收回时不切入原文案 */
  originEnd?: number;
}

/** AI 分段（来自 asr_summaries），时间单位为秒 */
export interface AiSegment {
  id: string;
  title: string;
  start: number;
  end: number;
}

export interface ManualSliceDraft {
  id: string;
  name: string;
  sourceVideoId: string;
  segments: SelectedCopySegment[];
  updatedAt: string;
}
