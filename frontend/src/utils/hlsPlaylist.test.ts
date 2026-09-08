import { describe, expect, it } from 'vitest';
import { playlistSegmentEpoch, stripRedundantHlsDiscontinuities } from './hlsPlaylist';

describe('stripRedundantHlsDiscontinuities', () => {
  it('去掉同一代数连续分片之间的 DISCONTINUITY', () => {
    const body = [
      '#EXTM3U',
      '#EXTINF:6.000,',
      'https://cdn.example/live-record/u/seg/1/seg_00000.ts',
      '#EXT-X-DISCONTINUITY',
      '#EXTINF:6.000,',
      'https://cdn.example/live-record/u/seg/1/seg_00001.ts',
      '',
    ].join('\n');

    const got = stripRedundantHlsDiscontinuities(body);
    expect(got).not.toContain('#EXT-X-DISCONTINUITY');
    expect(got).toContain('seg_00000.ts');
    expect(got).toContain('seg_00001.ts');
  });

  it('保留跨代数（续录）的 DISCONTINUITY', () => {
    const body = [
      '#EXTM3U',
      '#EXTINF:6.000,',
      'https://cdn.example/live-record/u/seg/1/seg_00009.ts',
      '#EXT-X-DISCONTINUITY',
      '#EXTINF:6.000,',
      'https://cdn.example/live-record/u/seg/2/seg_00010.ts',
    ].join('\n');

    expect(stripRedundantHlsDiscontinuities(body)).toContain('#EXT-X-DISCONTINUITY');
  });

  it('解析分片代数', () => {
    expect(playlistSegmentEpoch('https://cdn.example/seg/1/seg_00000.ts')).toBe('1');
    expect(playlistSegmentEpoch('https://cdn.example/seg_00000.ts')).toBe('');
  });
});
