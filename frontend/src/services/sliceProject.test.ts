import { describe, expect, it } from 'vitest';
import { clipsToSliceSegments, normalizeSliceProject, toSliceProjectClips } from './sliceProject';

describe('toSliceProjectClips', () => {
  it('字级时间由秒转毫秒并随 clips 提交', () => {
    const clips = toSliceProjectClips([
      {
        start: 11,
        end: 13,
        text: '注意到这个细节',
        words: [
          { start: 11, end: 11.5, text: '注意' },
          { start: 11.5, end: 11.7, text: '到' },
        ],
      },
    ]);

    expect(clips).toEqual([
      {
        start_time: 11000,
        end_time: 13000,
        text: '注意到这个细节',
        words: [
          { text: '注意', start_time: 11000, end_time: 11500 },
          { text: '到', start_time: 11500, end_time: 11700 },
        ],
      },
    ]);
  });

  it('裁掉片段区间外的词：被删掉的文字不会提交给后端', () => {
    const clips = toSliceProjectClips([
      {
        start: 11,
        end: 13,
        text: '注意到这个细节',
        words: [
          { start: 10.4, end: 11, text: '有没有' },
          { start: 11, end: 11.5, text: '注意' },
          { start: 13, end: 13.4, text: '呢' },
        ],
      },
    ]);

    expect(clips[0]!.words).toEqual([{ text: '注意', start_time: 11000, end_time: 11500 }]);
  });

  it('无字级时间时不写 words 字段', () => {
    const clips = toSliceProjectClips([{ start: 0, end: 1.005, text: '  好  ' }]);
    expect(clips[0]!.words).toBeUndefined();
    expect(clips[0]!.text).toBe('好');
    expect(clips[0]!.end_time).toBe(1005);
  });
});

describe('clipsToSliceSegments', () => {
  it('读回 clips1.words 并按秒回填片段', () => {
    const segments = clipsToSliceSegments({
      clips1: [
        {
          start_time: 10000,
          end_time: 13000,
          text: '注意到这个细节',
          words: [{ text: '注意', start_time: 11000, end_time: 11500 }],
        },
      ],
    });

    expect(segments).toHaveLength(1);
    expect(segments[0]!.start).toBe(10);
    expect(segments[0]!.end).toBe(13);
    expect(segments[0]!.words).toEqual([{ start: 11, end: 11.5, text: '注意' }]);
  });

  it('normalizeSliceProject 保留 clips1.words，二次保存不丢词级时间', () => {
    const project = normalizeSliceProject({
      id: 1,
      clips1: [
        {
          start_time: 0,
          end_time: 1000,
          text: '好',
          words: [{ text: '好', start_time: 100, end_time: 500 }],
        },
      ],
    });

    expect(project.clips1?.[0]?.words).toEqual([{ text: '好', start_time: 100, end_time: 500 }]);
  });
});
