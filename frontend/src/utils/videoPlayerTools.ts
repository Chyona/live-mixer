export interface VideoPlayerTranscriptWord {
  start: number;
  end: number;
  text: string;
}

export interface VideoPlayerTranscriptSegment {
  id: string;
  start: number;
  end: number;
  text: string;
  words?: VideoPlayerTranscriptWord[];
}

export interface VideoPlayerTranscriptParagraph {
  id: string;
  segments: VideoPlayerTranscriptSegment[];
}

export interface FlatTranscriptSegment {
  paragraphId: string;
  segmentId: string;
  start: number;
  end: number;
  text: string;
}

export const PLAYBACK_RATE_OPTIONS = [0.5, 0.75, 1, 1.25, 1.5, 2] as const;
export type PlaybackRateOption = (typeof PLAYBACK_RATE_OPTIONS)[number];

const PLAYBACK_SYNC_EPS = 0.05;

function isWhitespaceOnlyWordText(text: string): boolean {
  return text.length > 0 && [...text].every((ch) => /\s/u.test(ch));
}

function isTimingWord(word: VideoPlayerTranscriptWord): boolean {
  return (
    Number.isFinite(word.start) &&
    Number.isFinite(word.end) &&
    word.start >= 0 &&
    word.end >= 0 &&
    !isWhitespaceOnlyWordText(word.text)
  );
}

export function sanitizeScreenshotFilename(name: string) {
  return name.replace(/[\\/:*?"<>|]/g, '_').trim() || 'video-screenshot';
}

function getParagraphPlaybackStart(paragraph: VideoPlayerTranscriptParagraph) {
  return paragraph.segments[0]?.start ?? Number.POSITIVE_INFINITY;
}

/** 先定位当前段落，再在段落内定位子句，避免跨段落抢高亮。 */
function findActiveParagraph(
  paragraphs: VideoPlayerTranscriptParagraph[],
  currentTime: number
): VideoPlayerTranscriptParagraph | null {
  let best: { paragraph: VideoPlayerTranscriptParagraph; start: number } | null = null;

  for (const paragraph of paragraphs) {
    const start = getParagraphPlaybackStart(paragraph);
    if (start > currentTime + PLAYBACK_SYNC_EPS) continue;
    if (!best || start > best.start) {
      best = { paragraph, start };
    }
  }

  return best?.paragraph ?? null;
}

/**
 * 在单个段落内定位当前子句。
 * 优先用字级时间（in-range 取最晚起点；空隙内保持最近已开始字所在子句）；
 * 无字级时间时退回子句 start 不超过 currentTime 且最晚的一段。
 */
function findActiveSegmentInParagraph(
  paragraph: VideoPlayerTranscriptParagraph,
  currentTime: number
): string | null {
  const inRangeCandidates: Array<{ segmentId: string; start: number }> = [];
  let startedBest: { segmentId: string; start: number } | null = null;

  for (const segment of paragraph.segments) {
    if (!segment.words?.length) continue;
    if (segment.start > currentTime + PLAYBACK_SYNC_EPS) continue;

    for (const word of segment.words) {
      if (!isTimingWord(word)) continue;
      if (word.start > currentTime + PLAYBACK_SYNC_EPS) continue;

      if (!startedBest || word.start > startedBest.start) {
        startedBest = { segmentId: segment.id, start: word.start };
      }

      const wordEnd = word.end > word.start ? word.end : word.start;
      if (
        currentTime >= word.start - PLAYBACK_SYNC_EPS &&
        currentTime < wordEnd + PLAYBACK_SYNC_EPS
      ) {
        inRangeCandidates.push({ segmentId: segment.id, start: segment.start });
      }
    }
  }

  if (inRangeCandidates.length) {
    const inWindow = inRangeCandidates.filter((candidate) => {
      const segment = paragraph.segments.find((item) => item.id === candidate.segmentId);
      if (!segment) return false;
      const end = Math.max(segment.end, segment.start);
      return (
        currentTime >= segment.start - PLAYBACK_SYNC_EPS &&
        currentTime < end + PLAYBACK_SYNC_EPS
      );
    });
    const pool = inWindow.length ? inWindow : inRangeCandidates;
    const best = pool.reduce((acc, item) => (item.start > acc.start ? item : acc));
    return best.segmentId;
  }

  if (startedBest) return startedBest.segmentId;

  let segmentBest: { segmentId: string; start: number } | null = null;
  for (const segment of paragraph.segments) {
    if (segment.start > currentTime + PLAYBACK_SYNC_EPS) continue;
    if (!segmentBest || segment.start > segmentBest.start) {
      segmentBest = { segmentId: segment.id, start: segment.start };
    }
  }

  return segmentBest?.segmentId ?? null;
}

/**
 * 根据播放进度定位当前文案段。
 * 1. 段落级：取 start 不超过 currentTime 且最晚的段落（避免 ASR end 延伸到片尾误命中前序段）；
 * 2. 子句级：在段落内按字级时间定位，减少标点拆句后子句 start 不准导致的乱跳。
 */
export function findActiveSegment(
  paragraphs: VideoPlayerTranscriptParagraph[],
  currentTime: number
): { paragraphId: string; segmentId: string } | null {
  const paragraph = findActiveParagraph(paragraphs, currentTime);
  if (!paragraph) return null;

  const segmentId = findActiveSegmentInParagraph(paragraph, currentTime);
  if (!segmentId) return null;

  return { paragraphId: paragraph.id, segmentId };
}

export function flattenTranscriptSegments(
  paragraphs: VideoPlayerTranscriptParagraph[]
): FlatTranscriptSegment[] {
  return paragraphs
    .flatMap((paragraph) =>
      paragraph.segments.map((segment) => ({
        paragraphId: paragraph.id,
        segmentId: segment.id,
        start: segment.start,
        end: segment.end,
        text: segment.text,
      }))
    )
    .sort((a, b) => a.start - b.start || a.end - b.end);
}

/** 前一句 / 后一句：跳转到相邻文案子句的起点（秒） */
export function getAdjacentSentenceSeekTime(
  paragraphs: VideoPlayerTranscriptParagraph[],
  currentTime: number,
  direction: 'prev' | 'next'
): number | null {
  const flat = flattenTranscriptSegments(paragraphs);
  if (!flat.length) return null;

  const active = findActiveSegment(paragraphs, currentTime);
  const currentIndex = active
    ? flat.findIndex(
        (segment) =>
          segment.paragraphId === active.paragraphId && segment.segmentId === active.segmentId
      )
    : -1;

  if (direction === 'prev') {
    if (currentIndex > 0) return flat[currentIndex - 1]?.start ?? null;
    if (currentIndex === 0) return null;

    for (let i = flat.length - 1; i >= 0; i -= 1) {
      const segment = flat[i];
      if (segment && segment.start < currentTime - PLAYBACK_SYNC_EPS) {
        return segment.start;
      }
    }
    return null;
  }

  if (currentIndex >= 0 && currentIndex < flat.length - 1) {
    return flat[currentIndex + 1]?.start ?? null;
  }
  if (currentIndex === flat.length - 1) return null;

  const next = flat.find((segment) => segment.start > currentTime + PLAYBACK_SYNC_EPS);
  return next?.start ?? null;
}

export function canSeekAdjacentSentence(
  paragraphs: VideoPlayerTranscriptParagraph[],
  currentTime: number,
  direction: 'prev' | 'next'
): boolean {
  return getAdjacentSentenceSeekTime(paragraphs, currentTime, direction) != null;
}

export const SCREENSHOT_ERROR = {
  VIDEO_NOT_READY: 'VIDEO_NOT_READY',
  VIDEO_NO_FRAME: 'VIDEO_NO_FRAME',
  CORS_RESTRICTED: 'CORS_RESTRICTED',
  CAPTURE_FAILED: 'CAPTURE_FAILED',
  CANVAS_UNAVAILABLE: 'CANVAS_UNAVAILABLE',
} as const;

function isCrossOriginVideoSource(video: HTMLVideoElement): boolean {
  const src = video.currentSrc || video.src;
  if (!src || src.startsWith('blob:') || src.startsWith('data:')) {
    return false;
  }

  try {
    return new URL(src, window.location.href).origin !== window.location.origin;
  } catch {
    return false;
  }
}

export function getScreenshotErrorNotify(error: unknown): {
  type: 'warning' | 'error';
  title: string;
  description?: string;
} {
  const code = error instanceof Error ? error.message : '';

  if (code === SCREENSHOT_ERROR.VIDEO_NOT_READY) {
    return { type: 'warning', title: '视频尚未就绪，请稍后再试' };
  }

  if (code === SCREENSHOT_ERROR.VIDEO_NO_FRAME) {
    return { type: 'warning', title: '当前画面无法截取，请等待视频加载完成' };
  }

  if (code === SCREENSHOT_ERROR.CORS_RESTRICTED) {
    return {
      type: 'error',
      title: '截图失败',
      description:
        '视频存储未配置 CORS，请联系管理员在对象存储（TOS）中为站点域名开启跨域读取',
    };
  }

  return {
    type: 'error',
    title: '截图失败',
    description: '可能是跨域视频限制，请确认对象存储已配置 CORS',
  };
}

export async function captureVideoScreenshot(video: HTMLVideoElement, filename: string) {
  if (video.readyState < HTMLMediaElement.HAVE_CURRENT_DATA) {
    throw new Error(SCREENSHOT_ERROR.VIDEO_NOT_READY);
  }
  if (video.videoWidth <= 0 || video.videoHeight <= 0) {
    throw new Error(SCREENSHOT_ERROR.VIDEO_NO_FRAME);
  }

  const canvas = document.createElement('canvas');
  canvas.width = video.videoWidth;
  canvas.height = video.videoHeight;

  const ctx = canvas.getContext('2d');
  if (!ctx) {
    throw new Error(SCREENSHOT_ERROR.CANVAS_UNAVAILABLE);
  }

  try {
    ctx.drawImage(video, 0, 0, canvas.width, canvas.height);
  } catch (error) {
    if (error instanceof DOMException && error.name === 'SecurityError') {
      throw new Error(SCREENSHOT_ERROR.CORS_RESTRICTED);
    }
    throw error;
  }

  const blob = await new Promise<Blob | null>((resolve) => {
    canvas.toBlob(resolve, 'image/png');
  });
  if (!blob) {
    if (isCrossOriginVideoSource(video)) {
      throw new Error(SCREENSHOT_ERROR.CORS_RESTRICTED);
    }
    throw new Error(SCREENSHOT_ERROR.CAPTURE_FAILED);
  }

  const url = URL.createObjectURL(blob);
  const anchor = document.createElement('a');
  anchor.href = url;
  anchor.download = filename.endsWith('.png') ? filename : `${filename}.png`;
  anchor.click();
  URL.revokeObjectURL(url);
}
