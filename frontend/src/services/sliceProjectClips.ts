import type { TranscriptWord } from '~/pages/ManualVideoSlice/types';

/**
 * clips 载荷的类型与纯函数。
 *
 * 单独成模块，避免 mock（vite-plugin-mock 用 esbuild 打包）经由本文件
 * 间接引入 ./http 等依赖浏览器/UI 的代码。
 */

/** 接口 clips 字级时间单位：毫秒（与后端 model.ClipWord 一致） */
export interface SliceProjectClipWord {
  text: string;
  start_time: number;
  end_time: number;
}

/** 接口 clips 时间单位：毫秒 */
export interface SliceProjectClip {
  start_time: number;
  end_time: number;
  /** 人工切片文案（clips1） */
  text?: string;
  /** 时间轴片段标题（clips0，来自 asr_summaries.title） */
  title?: string;
  /**
   * 人工切片字级时间（clips1），与 text 对应。
   * 提交成片时后端据此生成字幕：字幕文本与画面一致，人工删除的文字不会被 ASR 回灌。
   */
  words?: SliceProjectClipWord[];
}

/** 提交 clips 的内部片段：时间单位为秒，words 可选 */
export interface SliceProjectClipInput {
  start: number;
  end: number;
  text?: string;
  /** 字级时间（秒），提交时转毫秒并裁剪到片段区间 */
  words?: TranscriptWord[];
}

/** 内部字级时间（秒）→ 接口字级时间（毫秒），只保留与片段区间有交集的词 */
function uiWordsToClipWords(
  words: TranscriptWord[] | undefined,
  start: number,
  end: number
): SliceProjectClipWord[] | undefined {
  if (!words?.length) return undefined;
  const out: SliceProjectClipWord[] = [];
  for (const word of words) {
    if (!word?.text?.trim()) continue;
    if (!Number.isFinite(word.start) || !Number.isFinite(word.end)) continue;
    // 只保留与片段区间有交集的词（片段被收窄/局部删除后，区间外的词不属于该片段）
    if (word.end <= start || word.start >= end) continue;
    const startTime = Math.round(word.start * 1000);
    const endTime = Math.round(word.end * 1000);
    if (endTime <= startTime) continue;
    out.push({ text: word.text, start_time: startTime, end_time: endTime });
  }
  return out.length ? out : undefined;
}

/** 内部秒级片段 → 接口毫秒级 clips */
export function toSliceProjectClips(segments: SliceProjectClipInput[]): SliceProjectClip[] {
  return segments.map((segment) => ({
    start_time: Math.round(segment.start * 1000),
    end_time: Math.round(segment.end * 1000),
    text: segment.text?.trim() || undefined,
    words: uiWordsToClipWords(segment.words, segment.start, segment.end),
  }));
}
