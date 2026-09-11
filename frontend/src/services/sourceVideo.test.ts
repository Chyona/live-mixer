import { afterEach, describe, expect, it } from 'vitest';
import { resetDebugSearchPersistForTests } from '~/utils/asrParagraphsKey';
import { normalizeSourceVideo } from './sourceVideo';
import { sourceVideoPlayUrl } from './sourceVideo.model';

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

describe('sourceVideoPlayUrl 跟播预览时间轴', () => {
  it('跟播不得回退到源站 m3u8', () => {
    expect(
      sourceVideoPlayUrl({
        play_url: 'https://src.example/live.m3u8',
        record_playlist_url: '',
        m3u8_url: 'https://src.example/live.m3u8',
        live_url: '',
        live_status: 'live',
      })
    ).toBe('');
  });

  it('仅一窗时优先播窗 MP4（与 ASR 同源）', () => {
    expect(
      sourceVideoPlayUrl({
        play_url: 'https://cdn.example/live.m3u8',
        record_playlist_url: 'https://cdn.example/live.m3u8',
        m3u8_url: 'https://src.example/live.m3u8',
        live_url: '',
        live_status: 'live',
        media_windows: [
          {
            i: 0,
            start_ms: 0,
            end_ms: 600000,
            dur_ms: 600000,
            url: 'https://cdn.example/windows/window_00000.mp4',
            ts_url: 'https://cdn.example/windows/window_00000.ts',
            ready: true,
          },
        ],
      })
    ).toBe('https://cdn.example/windows/window_00000.mp4');
  });

  it('多窗时使用录像 playlist', () => {
    expect(
      sourceVideoPlayUrl({
        play_url: 'https://cdn.example/live.m3u8',
        record_playlist_url: 'https://cdn.example/live.m3u8',
        m3u8_url: 'https://src.example/live.m3u8',
        live_url: '',
        live_status: 'live',
        media_windows: [
          {
            i: 0,
            start_ms: 0,
            end_ms: 600000,
            dur_ms: 600000,
            url: 'https://cdn.example/w0.mp4',
            ready: true,
          },
          {
            i: 1,
            start_ms: 600000,
            end_ms: 1200000,
            dur_ms: 600000,
            url: 'https://cdn.example/w1.mp4',
            ready: true,
          },
        ],
      })
    ).toBe('https://cdn.example/live.m3u8');
  });

  it('normalize 会纠正错误的 play_url', () => {
    const video = normalizeSourceVideo({
      id: 2,
      live_status: 'live',
      play_url: 'https://src.example/live.m3u8',
      m3u8_url: 'https://src.example/live.m3u8',
      record_playlist_url: '',
      media_windows: [
        {
          i: 0,
          start_ms: 0,
          end_ms: 602000,
          dur_ms: 602000,
          url: 'https://cdn.example/window_00000.mp4',
          ready: true,
        },
      ],
    });
    expect(video.play_url).toBe('https://cdn.example/window_00000.mp4');
  });
});
