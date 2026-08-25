import type { MockMethod } from 'vite-plugin-mock';
import { API_PREFIX } from './_config';
import { createAiSliceTask } from './clipTaskStore';
import { upsertSliceProject } from './sliceProjectStore';
import type { AsrParagraphs, AsrSummary, LiveAsrSegment } from '../src/services/sourceVideo.model';
import type {
  ManualSliceDraft,
  SelectedCopySegment,
  TranscriptParagraph,
} from '../src/pages/ManualVideoSlice/types';
import {
  asrParagraphsToTranscriptParagraphs,
  normalizeTranscriptParagraphs,
} from '../src/pages/ManualVideoSlice/utils';

const SPEAKERS = ['1', '2'];

const SCRIPT_LINES: Array<{ speaker: string; text: string; durationSec: number }> = [
  { speaker: '1', text: '大家好，欢迎来到今天的直播专场。', durationSec: 4 },
  { speaker: '1', text: '今天给大家带来的是春季上新系列，性价比非常高。', durationSec: 5 },
  { speaker: '2', text: '是的，这一季我们重点升级了面料和版型。', durationSec: 4.5 },
  { speaker: '2', text: '很多老用户反馈上身效果比上一代更舒适。', durationSec: 4.5 },
  { speaker: '1', text: '我们先看第一款爆款，经典百搭款，适合日常通勤。', durationSec: 5 },
  { speaker: '1', text: '现在下单还有限时优惠，库存真的不多。', durationSec: 4.5 },
  { speaker: '2', text: '这个颜色今年特别受欢迎，显白又高级。', durationSec: 4 },
  { speaker: '1', text: '评论区已经有朋友在问尺码问题了。', durationSec: 3.5 },
  { speaker: '1', text: '建议按照平时尺码选择，偏宽松可以选小一码。', durationSec: 4.5 },
  { speaker: '2', text: '另外搭配这条围巾，整体层次感会更好。', durationSec: 4 },
  { speaker: '1', text: '接下来这款是今天的主推，限时秒杀价。', durationSec: 4.5 },
  { speaker: '1', text: '它的核心卖点是轻便、防泼水，非常适合户外。', durationSec: 5 },
  { speaker: '2', text: '我们现场做了一个小测试，泼水几乎不留痕。', durationSec: 4.5 },
  { speaker: '1', text: '想要的朋友可以直接点购物车，手慢无。', durationSec: 4 },
  { speaker: '2', text: '售后方面同样支持七天无理由，大家放心买。', durationSec: 4.5 },
  { speaker: '1', text: '最后提醒一下，关注直播间可领额外优惠券。', durationSec: 4.5 },
  { speaker: '1', text: '感谢大家的支持，我们下一轮福利马上开始。', durationSec: 4.5 },
];

function textToWords(text: string, startMs: number, endMs: number) {
  const chars = Array.from(text);
  if (!chars.length) return [];

  const duration = Math.max(endMs - startMs, chars.length);
  let cursor = startMs;

  return chars.map((char, index) => {
    const wordEnd = index === chars.length - 1 ? endMs : Math.round(cursor + duration / chars.length);
    const word = {
      start_time: Math.round(cursor),
      end_time: wordEnd,
      text: char,
    };
    cursor = wordEnd;
    return word;
  });
}

function buildAsrParagraphs(durationSec = 600): AsrParagraphs {
  const segments: LiveAsrSegment[] = [];
  let cursorSec = 0.82;
  let lineIndex = 0;

  while (cursorSec < durationSec - 2 && lineIndex < SCRIPT_LINES.length * 20) {
    const line = SCRIPT_LINES[lineIndex % SCRIPT_LINES.length];
    const speaker = SPEAKERS.includes(line.speaker) ? line.speaker : SPEAKERS[0];
    const lineDuration = Math.min(line.durationSec, durationSec - cursorSec);
    const startMs = Math.round(cursorSec * 1000);
    const endMs = Math.round((cursorSec + lineDuration) * 1000);

    segments.push({
      speaker,
      start_time: startMs,
      end_time: endMs,
      text: line.text,
      words: textToWords(line.text, startMs, endMs),
    });

    cursorSec = cursorSec + lineDuration + 0.3;
    lineIndex += 1;
  }

  return segments;
}

const asrParagraphsCache = new Map<string, AsrParagraphs>();
const draftStore = new Map<string, ManualSliceDraft[]>();

export function getTranscript(sourceVideoId: string): AsrParagraphs {
  if (!asrParagraphsCache.has(sourceVideoId)) {
    asrParagraphsCache.set(sourceVideoId, buildAsrParagraphs());
  }
  return asrParagraphsCache.get(sourceVideoId)!;
}

/** mock：按文案分段拼几段摘要，供时间轴无 clips0 时回填 */
export function getAsrSummaries(sourceVideoId: string): AsrSummary[] {
  const paragraphs = getTranscript(sourceVideoId);
  if (!paragraphs.length) return [];

  const chunkSize = Math.max(3, Math.ceil(paragraphs.length / 4));
  const summaries: AsrSummary[] = [];

  for (let i = 0; i < paragraphs.length; i += chunkSize) {
    const chunk = paragraphs.slice(i, i + chunkSize);
    const start = chunk[0]?.start_time ?? 0;
    const end = chunk[chunk.length - 1]?.end_time ?? start;
    if (end <= start) continue;
    summaries.push({
      title: `摘要 ${summaries.length + 1}`,
      start_time: start,
      end_time: end,
    });
  }

  return summaries;
}

function overlapsRange(start: number, end: number, clipStart: number, clipEnd: number) {
  return end > clipStart && start < clipEnd;
}

function buildAiSelectedSegments(
  paragraphs: TranscriptParagraph[],
  clips: Array<{ start: number; end: number }>
): SelectedCopySegment[] {
  const selected: SelectedCopySegment[] = [];
  const seen = new Set<string>();

  for (const clip of clips) {
    for (const paragraph of paragraphs) {
      for (const segment of paragraph.segments) {
        if (!overlapsRange(segment.start, segment.end, clip.start, clip.end)) continue;
        if (seen.has(segment.id)) continue;

        seen.add(segment.id);
        selected.push({
          id: segment.id,
          speakerId: paragraph.speakerId,
          speakerName: paragraph.speakerName,
          text: segment.text,
          start: segment.start,
          end: segment.end,
        });
      }
    }
  }

  return selected.sort((a, b) => a.start - b.start);
}

export default [
  {
    url: `${API_PREFIX}/v1/source-videos/:id/transcript`,
    method: 'get',
    response: ({ query }: { query: { id: string } }) => {
      return { code: 0, message: '', data: getTranscript(query.id) };
    },
  },
  {
    url: `${API_PREFIX}/v1/source-videos/:id/manual-slice-drafts`,
    method: 'get',
    response: ({ query }: { query: { id: string } }) => {
      return {
        code: 0,
        message: '',
        data: draftStore.get(query.id) ?? [],
      };
    },
  },
  {
    url: `${API_PREFIX}/v1/source-videos/:id/manual-slice-drafts`,
    method: 'post',
    response: ({
      body,
      query,
    }: {
      body: { name?: string; segments?: ManualSliceDraft['segments'] };
      query: { id: string };
    }) => {
      const name = body?.name?.trim();
      if (!name) {
        return { code: 400, message: '请输入项目名称', data: null };
      }

      const draft: ManualSliceDraft = {
        id: `draft-${Date.now()}`,
        name,
        sourceVideoId: query.id,
        segments: body?.segments ?? [],
        updatedAt: new Date().toISOString(),
      };

      const list = draftStore.get(query.id) ?? [];
      list.unshift(draft);
      draftStore.set(query.id, list);

      upsertSliceProject({
        sourceVideoId: query.id,
        projectName: `${name} 剪辑项目`,
        segmentCount: draft.segments.length,
        segments: draft.segments,
      });

      return { code: 0, message: '', data: draft };
    },
  },
  {
    url: `${API_PREFIX}/v1/source-videos/:id/ai-slice-select`,
    method: 'post',
    response: ({
      body,
      query,
    }: {
      body: {
        prompt?: string;
        promptId?: string;
        clips?: Array<{ start: number; end: number }>;
        sourceVideoName?: string;
      };
      query: { id: string };
    }) => {
      const prompt = body?.prompt?.trim();
      const clips = body?.clips ?? [];

      if (!prompt) {
        return { code: 400, message: '请选择 AI 提示词', data: null };
      }

      if (!clips.length) {
        return { code: 400, message: '请选择至少一个时间片段', data: null };
      }

      const paragraphs = normalizeTranscriptParagraphs(
        asrParagraphsToTranscriptParagraphs(getTranscript(query.id))
      );
      const segments = buildAiSelectedSegments(paragraphs, clips);

      if (!segments.length) {
        return { code: 400, message: '所选时间范围内未匹配到文案片段', data: null };
      }

      const taskId = `ai-slice-task-${Date.now()}`;
      createAiSliceTask({
        taskId,
        sourceVideoId: query.id,
        sourceVideoName: body?.sourceVideoName?.trim() || `源视频 ${query.id}`,
        promptName: prompt.slice(0, 24),
        clips,
        segments,
      });

      return {
        code: 0,
        message: '',
        data: { taskId },
      };
    },
  },
] as MockMethod[];
