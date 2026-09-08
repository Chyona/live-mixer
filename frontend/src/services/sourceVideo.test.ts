import { afterEach, describe, expect, it } from 'vitest';
import { resetDebugSearchPersistForTests } from '~/utils/asrParagraphsKey';
import { normalizeSourceVideo } from './sourceVideo';

function setSearch(search: string) {
  window.history.replaceState({}, '', search ? `/${search}` : '/');
}

const liveUtterance = {
  speaker: '1',
  start_time: 2780,
  end_time: 4580,
  text: '这里挺整洁的',
};

const paragraph = {
  speaker: '1',
  start_time: 1000,
  end_time: 4000,
  text: '合并后的段落',
};

describe('normalizeSourceVideo ASR 字段', () => {
  afterEach(() => {
    setSearch('');
    resetDebugSearchPersistForTests();
  });

  it('跟播中 asr_paragraphs 为空时回退到 live_asr', () => {
    const video = normalizeSourceVideo({
      id: 9,
      asr_paragraphs: [],
      live_asr: [liveUtterance],
    });
    expect(video.asr_paragraphs).toEqual([liveUtterance]);
  });

  it('关播后优先使用 asr_paragraphs', () => {
    const video = normalizeSourceVideo({
      id: 9,
      asr_paragraphs: [paragraph],
      live_asr: [liveUtterance],
    });
    expect(video.asr_paragraphs).toEqual([paragraph]);
  });

  it('isdebug=true 时优先 live_asr', () => {
    setSearch('?isdebug=true');
    const video = normalizeSourceVideo({
      id: 9,
      asr_paragraphs: [paragraph],
      live_asr: [liveUtterance],
    });
    expect(video.asr_paragraphs).toEqual([liveUtterance]);
  });
});
