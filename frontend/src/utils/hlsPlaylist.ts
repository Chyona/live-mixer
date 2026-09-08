/** 从分片 URL 取出跟播目录代数（.../seg/{epoch}/seg_00000.ts）。 */
export function playlistSegmentEpoch(url: string): string {
  const match = url.trim().match(/\/seg\/(\d+)\//);
  return match?.[1] ?? '';
}

/**
 * 去掉同一录像代数内多余的 #EXT-X-DISCONTINUITY。
 * 历史录像曾在每 6s 分片之间插入该标记，hls.js 会把清单加载判成网络失败。
 */
export function stripRedundantHlsDiscontinuities(playlist: string): string {
  const lines = playlist.split(/\r?\n/);
  const out: string[] = [];
  let pendingDiscontinuity = false;
  let lastEpoch = '';

  for (const line of lines) {
    const trimmed = line.trim();
    if (trimmed === '#EXT-X-DISCONTINUITY') {
      pendingDiscontinuity = true;
      continue;
    }

    const isURI = trimmed.length > 0 && !trimmed.startsWith('#');
    if (isURI) {
      const epoch = playlistSegmentEpoch(trimmed);
      if (pendingDiscontinuity && lastEpoch && epoch && epoch !== lastEpoch) {
        out.push('#EXT-X-DISCONTINUITY');
      }
      pendingDiscontinuity = false;
      if (epoch) lastEpoch = epoch;
    }

    out.push(line);
  }

  return out.join('\n');
}
