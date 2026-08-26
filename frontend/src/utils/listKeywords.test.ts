import { describe, expect, it } from 'vitest';
import {
  highlightListKeywords,
  highlightPlainKeyword,
  normalizeKeywordSeparators,
  parseHighlightKeywords,
  parseListKeywords,
  toApiKeywords,
} from './listKeywords';

describe('normalizeKeywordSeparators', () => {
  it('converts fullwidth pipe and ideographic comma to comma', () => {
    expect(normalizeKeywordSeparators('财富｜格式')).toBe('财富|格式');
    expect(normalizeKeywordSeparators('A、B')).toBe('A,B');
  });
});

describe('parseListKeywords', () => {
  it('splits on Chinese and fullwidth separators', () => {
    expect(parseListKeywords('财富｜格式')).toEqual(['财富', '格式']);
    expect(parseListKeywords('关键词A+关键词B')).toEqual(['关键词A', '关键词B']);
  });
});

describe('parseHighlightKeywords', () => {
  it('splits OR and AND queries for highlighting', () => {
    expect(parseHighlightKeywords('财富|康波周期')).toEqual(['财富', '康波周期']);
    expect(parseHighlightKeywords('财富+康波周期')).toEqual(['财富', '康波周期']);
  });
});

describe('toApiKeywords', () => {
  it('keeps English separators without comma joining', () => {
    expect(toApiKeywords('财富｜格式')).toBe('财富|格式');
    expect(toApiKeywords('关键词A＋关键词B')).toBe('关键词A+关键词B');
  });
});

describe('highlightListKeywords', () => {
  it('highlights each term for OR search with pipe', () => {
    const keywords = parseHighlightKeywords('财富|康波周期');
    expect(highlightListKeywords('我们聊聊财富增长与康波周期理论', keywords)).toBe(
      '我们聊聊<span class="list-keyword-hit">财富</span>增长与<span class="list-keyword-hit">康波周期</span>理论'
    );
  });

  it('re-parses unsplit OR keyword strings defensively', () => {
    expect(
      highlightListKeywords('我们聊聊财富增长与康波周期理论', ['财富|康波周期'])
    ).toBe(
      '我们聊聊<span class="list-keyword-hit">财富</span>增长与<span class="list-keyword-hit">康波周期</span>理论'
    );
  });
});

describe('highlightPlainKeyword', () => {
  it('highlights the literal keyword without splitting on pipe', () => {
    expect(highlightPlainKeyword('A|B 与 A', 'A|B')).toBe(
      '<span class="list-keyword-hit">A|B</span> 与 A'
    );
  });
});
