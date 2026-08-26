import { describe, expect, it } from 'vitest';
import type { LiveAsrSegment, LiveAsrWord } from '~/services/sourceVideo.model';
import {
  flattenSegmentWords,
  locateWordInSegments,
  parseLiveAsrSegments,
  verifyAsrParagraphsAgainstLiveAsr,
} from './asrParagraphsVerify';

function word(text: string, start: number, end: number): LiveAsrWord {
  return { text, start_time: start, end_time: end };
}

function seg(
  text: string,
  start: number,
  end: number,
  options?: { speaker?: string; words?: LiveAsrWord[] }
): LiveAsrSegment {
  return {
    text,
    start_time: start,
    end_time: end,
    speaker: options?.speaker ?? '1',
    words: options?.words,
  };
}

describe('verifyAsrParagraphsAgainstLiveAsr', () => {
  it('passes when flattened words match one-to-one across fields', () => {
    const words = [
      word('新', 1000, 1100),
      word('能源', 1100, 1200),
      word('汽车', 1200, 1400),
    ];
    const liveAsr = [
      seg('新', 1000, 1100, { words: [words[0]!] }),
      seg('能源', 1100, 1200, { words: [words[1]!] }),
      seg('汽车', 1200, 1400, { words: [words[2]!] }),
    ];
    const asrParagraphs = [seg('新能源汽车', 1000, 1400, { words })];

    const report = verifyAsrParagraphsAgainstLiveAsr(asrParagraphs, liveAsr);

    expect(report.ok).toBe(true);
    expect(report.firstIssue).toBeNull();
    expect(flattenSegmentWords(asrParagraphs).map((item) => item.text).join('')).toBe('新能源汽车');
  });

  it('passes when paragraphs merge live_asr short segments in order', () => {
    const liveWords = [
      word('新', 1000, 1050),
      word('能', 1050, 1100),
      word('源', 1100, 1200),
      word('汽', 1200, 1300),
      word('车', 1300, 1400),
      word('跌', 1400, 1500),
      word('了', 1500, 1600),
      word('14', 1600, 1700),
      word('%', 1700, 1800),
    ];
    const liveAsr = [
      seg('新能源', 1000, 1200, { words: liveWords.slice(0, 3) }),
      seg('汽车', 1200, 1400, { words: liveWords.slice(3, 5) }),
      seg('跌', 1400, 1500, { words: [liveWords[5]!] }),
      seg('了', 1500, 1600, { words: [liveWords[6]!] }),
      seg('14%', 1600, 1800, { words: liveWords.slice(7) }),
    ];
    const asrParagraphs = [seg('新能源汽车跌了14%', 1000, 1800, { words: liveWords })];

    const report = verifyAsrParagraphsAgainstLiveAsr(asrParagraphs, liveAsr);

    expect(report.ok).toBe(true);
    expect(report.firstIssue).toBeNull();
  });

  it('reports word count mismatch as first issue when flattened lengths differ', () => {
    const asrParagraphs = [
      seg('你好', 0, 1000, { words: [word('你', 0, 500), word('好', 500, 1000)] }),
    ];
    const liveAsr = [seg('你好', 0, 1000, { words: [word('你', 0, 500)] })];

    const report = verifyAsrParagraphsAgainstLiveAsr(asrParagraphs, liveAsr);

    expect(report.ok).toBe(false);
    expect(report.firstIssue?.message).toContain('words 数量不一致');
  });

  it('reports only the first word timing mismatch', () => {
    const asrParagraphs = [
      seg('AI', 0, 200, { words: [word('AI', 0, 200)] }),
    ];
    const liveAsr = [
      seg('AI', 0, 200, { words: [word('AI', 100, 200)] }),
    ];

    const report = verifyAsrParagraphsAgainstLiveAsr(asrParagraphs, liveAsr);

    expect(report.ok).toBe(false);
    expect(report.firstIssue?.message).toContain('start_time');
    expect(report.firstIssue?.wordIndex).toBe(0);
  });

  it('locates flattened word index back to owning segment on each side', () => {
    const words = [
      word('你', 0, 100),
      word('好', 100, 200),
      word('世', 200, 300),
      word('界', 300, 400),
    ];
    const liveAsr = [
      seg('你好', 0, 200, { words: words.slice(0, 2) }),
      seg('世界', 200, 400, { words: words.slice(2) }),
    ];
    const asrParagraphs = [seg('你好世界', 0, 400, { words })];

    expect(locateWordInSegments(asrParagraphs, 2)).toEqual({
      segmentIndex: 0,
      wordIndexInSegment: 2,
      text: '你好世界',
      start_time: 0,
      end_time: 400,
      speaker: '1',
    });
    expect(locateWordInSegments(liveAsr, 2)).toEqual({
      segmentIndex: 1,
      wordIndexInSegment: 0,
      text: '世界',
      start_time: 200,
      end_time: 400,
      speaker: '1',
    });
  });

  it('reports paragraph text mismatch when words match but merge differs', () => {
    const liveWords = [word('你', 0, 250), word('好', 250, 500), word('世', 500, 750), word('界', 750, 1000)];
    const liveAsr = [
      seg('你好', 0, 500, { words: liveWords.slice(0, 2) }),
      seg('世界', 500, 1000, { words: liveWords.slice(2) }),
    ];
    const asrParagraphs = [
      seg('你好地球', 0, 1000, { words: liveWords }),
    ];

    const report = verifyAsrParagraphsAgainstLiveAsr(asrParagraphs, liveAsr);

    expect(report.ok).toBe(false);
    expect(report.firstIssue?.message).toContain('文案与 live_asr 子句合并不一致');
  });

  it('keeps empty-text words when flattening', () => {
    const asrParagraphs = parseLiveAsrSegments([
      {
        speaker: '1',
        text: 'A',
        start_time: 0,
        end_time: 100,
        words: [
          { text: 'A', start_time: 0, end_time: 50 },
          { text: '', start_time: 50, end_time: 100 },
        ],
      },
    ]);

    expect(flattenSegmentWords(asrParagraphs)).toEqual([
      { text: 'A', start_time: 0, end_time: 50 },
      { text: '', start_time: 50, end_time: 100 },
    ]);
  });

  it('reports paragraph end_time mismatch with paragraph index not word index', () => {
    const liveAsr = [
      seg('你好', 0, 500, { words: [word('你', 0, 250), word('好', 250, 500)] }),
      seg('世界', 500, 900, { words: [word('世', 500, 700), word('界', 700, 900)] }),
    ];
    const asrParagraphs = [seg('你好世界', 0, 1000, {
      words: [word('你', 0, 250), word('好', 250, 500), word('世', 500, 700), word('界', 700, 900)],
    })];

    const report = verifyAsrParagraphsAgainstLiveAsr(asrParagraphs, liveAsr);

    expect(report.ok).toBe(false);
    expect(report.firstIssue?.wordIndex).toBeUndefined();
    expect(report.firstIssue?.paragraphIndex).toBe(0);
    expect(report.firstIssue?.message).toContain('end_time 不一致');
    expect(report.firstIssue?.paragraph).toMatchObject({
      text: '你好世界',
      end_time: 1000,
    });
    expect(report.firstIssue?.live).toMatchObject({
      text: '你好世界',
      end_time: 900,
    });
    expect(report.firstIssue?.live).toMatchObject({
      segments: [
        {
          text: '你好',
          start_time: 0,
          end_time: 500,
          words: [word('你', 0, 250), word('好', 250, 500)],
        },
        {
          text: '世界',
          start_time: 500,
          end_time: 900,
          words: [word('世', 500, 700), word('界', 700, 900)],
        },
      ],
    });
    expect(report.firstIssue?.paragraph).toMatchObject({
      words: [word('你', 0, 250), word('好', 250, 500), word('世', 500, 700), word('界', 700, 900)],
    });
    expect(report.firstIssue?.liveIndexStart).toBe(0);
    expect(report.firstIssue?.liveIndexEnd).toBe(1);
  });

  it('ignores timing mismatch for whitespace-only words with matching text', () => {
    const asrParagraphs = [
      seg('A B', 0, 300, {
        words: [word('A', 0, 100), word(' ', 100, 200), word('B', 200, 300)],
      }),
    ];
    const liveAsr = [
      seg('A B', 0, 300, {
        words: [word('A', 0, 100), word(' ', -1, -1), word('B', 200, 300)],
      }),
    ];

    const report = verifyAsrParagraphsAgainstLiveAsr(asrParagraphs, liveAsr);

    expect(report.ok).toBe(true);
    expect(report.firstIssue).toBeNull();
  });

  it('reports orphan live segments not covered by paragraphs', () => {
    const liveWords = [word('A', 0, 50), word('B', 100, 150), word('C', 200, 250)];
    const liveAsr = [
      seg('A', 0, 100, { words: [liveWords[0]!] }),
      seg('B', 100, 200, { words: [liveWords[1]!] }),
      seg('C', 200, 300, { words: [liveWords[2]!] }),
    ];
    const asrParagraphs = [seg('AB', 0, 200, { words: liveWords })];

    const report = verifyAsrParagraphsAgainstLiveAsr(asrParagraphs, liveAsr);

    expect(report.ok).toBe(false);
    expect(report.firstIssue?.message).toContain('未并入 asr_paragraphs');
  });
});
