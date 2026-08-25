import type { MockMethod } from 'vite-plugin-mock';
import {
  createInitialAsrState,
  SOURCE_VIDEO_URL_DUPLICATE_CODE,
  type SourceVideo,
} from '../src/services/sourceVideo.model';
import {
  matchAnyListKeyword,
  matchListKeywords,
  parseListKeywords,
} from '../src/utils/listKeywords';
import { API_PREFIX } from './_config';
import { LIVE_URL } from './_Live_URL';
import { getAsrSummaries, getTranscript } from './transcript';
import { countSliceProjectsBySourceVideoId } from './sliceProjectStore';

type MockSourceVideo = Omit<
  SourceVideo,
  'project_count' | 'matched_paragraphs' | 'asr_paragraphs' | 'asr_summaries'
> & {
  ownerId: string;
  /** 节流 ASR 推进，避免列表轮询几秒内全部跑完 */
  _lastAsrTickMs?: number;
  /** 等待解析停留次数，避免首屏 pending 一刷新就消失 */
  _pendingTicks?: number;
};

const CURRENT_USER_ID = '222';

const MOCK_LIVE_TEMPLATES = [
  { name: '周末游戏直播回放', remark: '游戏专场素材' },
  { name: '新品发布会直播', remark: '发布会实录' },
  { name: '户外探店直播', remark: '探店精选' },
  { name: '美食烹饪教学', remark: '厨房直播' },
  { name: '数码产品测评', remark: '测评专场' },
  { name: '音乐演唱会直播', remark: '演唱会素材' },
  { name: '健身跟练直播', remark: '运动健身' },
  { name: '旅游风景直播', remark: '风景记录' },
  { name: '电商带货专场', remark: '带货素材' },
  { name: '访谈节目直播', remark: '访谈备用' },
  { name: '校园活动直播', remark: '校园活动' },
  { name: '科技峰会直播', remark: '峰会实录' },
  { name: '宠物日常直播', remark: '萌宠日常' },
  { name: '手工制作教学', remark: '手工教学' },
  { name: '读书分享会', remark: '读书分享' },
  { name: '汽车试驾直播', remark: '试驾记录' },
  { name: '时装秀场直播', remark: '秀场素材' },
  { name: '电竞赛事直播', remark: '赛事回放' },
  { name: '农业科普直播', remark: '科普专场' },
  { name: '城市夜景直播', remark: '夜景素材' },
];

const DEMO_VIDEO_URLS = [...LIVE_URL];

const FAILED_ASR_ERROR_MESSAGES = [
  '下载直播素材失败: write file: no space left on device',
  '转写服务超时，请稍后重试',
  'ASR 引擎返回错误: invalid audio stream',
];

function isoAt(day: string, hour = 10, minute = 0, second = 0) {
  const hh = String(hour).padStart(2, '0');
  const mm = String(minute).padStart(2, '0');
  const ss = String(second).padStart(2, '0');
  return `2026-06-${day}T${hh}:${mm}:${ss}.000000+08:00`;
}

function nowIso() {
  const date = new Date();
  const pad = (value: number, length = 2) => String(value).padStart(length, '0');
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}:${pad(date.getSeconds())}.${pad(date.getMilliseconds(), 3)}000+08:00`;
}

function resolveMockAsrState(
  index: number,
  created_at: string
): Pick<SourceVideo, 'asr_status' | 'asr_progress' | 'asr_error_msg' | 'asr_started_at' | 'asr_updated_at' | 'duration'> {
  // 演示数据以「等待 / 处理中 / 失败」为主，方便对照三种态
  const mod = index % 5;

  if (mod === 0) {
    return {
      asr_status: 'failed',
      asr_progress: 10,
      asr_error_msg: FAILED_ASR_ERROR_MESSAGES[index % FAILED_ASR_ERROR_MESSAGES.length],
      asr_started_at: created_at,
      asr_updated_at: created_at,
      duration: 0,
    };
  }

  if (mod === 1 || mod === 2) {
    return {
      ...createInitialAsrState(),
      duration: 0,
    };
  }

  if (mod === 3) {
    return {
      asr_status: 'processing',
      asr_progress: 18 + (index % 4) * 6,
      asr_error_msg: '',
      asr_started_at: created_at,
      asr_updated_at: created_at,
      duration: 0,
    };
  }

  return {
    asr_status: 'processing',
    asr_progress: 52 + (index % 5) * 7,
    asr_error_msg: '',
    asr_started_at: created_at,
    asr_updated_at: created_at,
    duration: 0,
  };
}

function buildMockSourceVideos(): MockSourceVideo[] {
  return MOCK_LIVE_TEMPLATES.map((item, index) => {
    const id = index + 1;
    const day = String((index % 28) + 1).padStart(2, '0');
    const created_at = isoAt(day, 10 + (index % 8), index % 60);

    return {
      id,
      name: item.name,
      live_url: DEMO_VIDEO_URLS[index % DEMO_VIDEO_URLS.length],
      remark: item.remark,
      ext: '',
      created_at,
      updated_at: created_at,
      created_by: '管理员',
      ownerId: CURRENT_USER_ID,
      ...resolveMockAsrState(index, created_at),
    };
  });
}

const sourceVideos: MockSourceVideo[] = [
  ...buildMockSourceVideos(),
  {
    id: 999,
    name: '其他用户直播素材',
    live_url: 'rtmp://live.example.com/stream/other',
    remark: '不应展示',
    duration: 1200 * 1000,
    ext: '',
    asr_error_msg: '',
    asr_started_at: isoAt('05', 12),
    asr_updated_at: isoAt('05', 12),
    asr_status: 'completed',
    asr_progress: 100,
    created_at: isoAt('05', 12),
    updated_at: isoAt('05', 12),
    created_by: '其他用户',
    ownerId: 'other-user',
  },
];

function toPublicItem(item: MockSourceVideo): SourceVideo {
  const {
    ownerId: _ownerId,
    _lastAsrTickMs: _tick,
    _pendingTicks: _pending,
    ...rest
  } = item;
  return {
    ...rest,
    project_count: countSliceProjectsBySourceVideoId(item.id),
    matched_paragraphs: null,
    asr_paragraphs: null,
    asr_summaries: null,
  };
}

/** 收集含任一 asr 关键词的命中段落，字段对齐接口 matched_paragraphs */
function collectMatchedParagraphs(
  sourceVideoId: string | number,
  asrKeywords: string[],
  max = 12
): SourceVideo['matched_paragraphs'] {
  if (!asrKeywords.length) return [];
  const hits: NonNullable<SourceVideo['matched_paragraphs']> = [];
  const seen = new Set<string>();

  for (const segment of getTranscript(String(sourceVideoId))) {
    const text = segment.text.trim();
    if (!text || seen.has(text)) continue;
    if (!matchAnyListKeyword(text, asrKeywords)) continue;
    seen.add(text);
    hits.push({
      speaker: segment.speaker,
      start_time: segment.start_time,
      end_time: segment.end_time,
      text,
    });
    if (hits.length >= max) break;
  }

  return hits;
}

function toPublicDetail(item: MockSourceVideo): SourceVideo {
  const base = toPublicItem(item);
  if (item.asr_status !== 'completed') {
    return { ...base, asr_paragraphs: null, asr_summaries: null };
  }
  return {
    ...base,
    asr_paragraphs: getTranscript(String(item.id)),
    asr_summaries: getAsrSummaries(String(item.id)),
  };
}

const ASR_TICK_INTERVAL_MS = 3000;

function advanceAsrProgress(item: MockSourceVideo) {
  if (item.asr_status === 'completed' || item.asr_status === 'failed') return;

  const tickAt = Date.now();
  if (item._lastAsrTickMs && tickAt - item._lastAsrTickMs < ASR_TICK_INTERVAL_MS) return;
  item._lastAsrTickMs = tickAt;

  const now = nowIso();

  if (item.asr_status === 'pending') {
    item._pendingTicks = (item._pendingTicks ?? 0) + 1;
    // 约停留 5 次 tick（~15s），方便查看「等待解析」态
    if (item._pendingTicks < 5) return;

    item.asr_status = 'processing';
    item.asr_progress = 6;
    item.asr_started_at = now;
    item.asr_updated_at = now;
    return;
  }

  // 演示数据推进更慢，避免很快全部变成「已完成」
  item.asr_progress = Math.min(100, item.asr_progress + 2 + Math.floor(Math.random() * 3));

  if (item.asr_progress >= 100) {
    item.asr_progress = 100;
    item.asr_status = 'completed';
    item.asr_error_msg = '';
    if (item.duration === 0) {
      item.duration = (1800 + Math.floor(Math.random() * 3600)) * 1000;
    }
  }

  item.asr_updated_at = now;
  item.updated_at = now;
}

function tickAllAsrJobs() {
  sourceVideos.forEach((item) => {
    if (item.ownerId !== CURRENT_USER_ID) return;
    advanceAsrProgress(item);
  });
}

function filterList(query: Record<string, string | string[] | undefined>) {
  const startDate =
    typeof query.start_date === 'string'
      ? query.start_date
      : typeof query.date === 'string'
        ? query.date
        : undefined;
  const endDate =
    typeof query.end_date === 'string'
      ? query.end_date
      : typeof query.dateEnd === 'string'
        ? query.dateEnd
        : undefined;
  const keyword =
    typeof query.keywords === 'string'
      ? query.keywords
      : typeof query.keyword === 'string'
        ? query.keyword
        : typeof query.title_keyword === 'string'
          ? query.title_keyword
          : undefined;
  const asrKeyword =
    typeof query.asr_keywords === 'string'
      ? query.asr_keywords
      : typeof query.asr_keyword === 'string'
        ? query.asr_keyword
        : typeof query.global_keyword === 'string'
          ? query.global_keyword
          : typeof query.globalKeyword === 'string'
            ? query.globalKeyword
            : undefined;
  const keywords = parseListKeywords(keyword);
  const asrKeywords = parseListKeywords(asrKeyword);

  return sourceVideos.filter((item) => {
    if (item.ownerId !== CURRENT_USER_ID) return false;

    const createdDate = item.created_at.slice(0, 10);
    if (startDate && createdDate < startDate) return false;
    if (endDate && createdDate > endDate) return false;

    const titleText = `${item.name} ${item.remark}`;
    if (!matchListKeywords(titleText, keywords)) return false;

    // asr_keywords：仅匹配 ASR 解析后的视频文案（未完成解析则无文案可命中）
    const asrText =
      item.asr_status === 'completed'
        ? getTranscript(String(item.id))
            .map((segment) => segment.text)
            .join(' ')
        : '';
    if (!matchListKeywords(asrText, asrKeywords)) return false;

    return true;
  });
}

export default [
  {
    url: `${API_PREFIX}/v1/live-materials`,
    method: 'get',
    response: ({ query }: { query: Record<string, string | string[] | undefined> }) => {
      tickAllAsrJobs();
      const page = Number(query.page || 1);
      const pageSize = Number(query.page_size || query.pageSize || 10);
      const filtered = filterList(query);
      const start = (page - 1) * pageSize;
      const asrKeywords = parseListKeywords(
        typeof query.asr_keywords === 'string'
          ? query.asr_keywords
          : typeof query.asr_keyword === 'string'
            ? query.asr_keyword
            : typeof query.global_keyword === 'string'
              ? query.global_keyword
              : undefined
      );

      return {
        code: 0,
        message: '',
        data: {
          list: filtered.slice(start, start + pageSize).map((item) => {
            const publicItem = toPublicItem(item);
            if (!asrKeywords.length || item.asr_status !== 'completed') {
              return publicItem;
            }
            return {
              ...publicItem,
              matched_paragraphs: collectMatchedParagraphs(item.id, asrKeywords),
            };
          }),
          total: filtered.length,
        },
      };
    },
  },
  {
    url: `${API_PREFIX}/v1/live-materials`,
    method: 'post',
    /** 重复 URL 需返回非 2xx，才能被 http 拦截器转为 AppError（与线上一致） */
    rawResponse: async (req, res) => {
      let raw = '';
      await new Promise<void>((resolve) => {
        req.on('data', (chunk) => {
          raw += chunk;
        });
        req.on('end', () => resolve());
      });

      let body: { name?: string; live_url?: string; remark?: string } = {};
      try {
        body = JSON.parse(raw || '{}') as typeof body;
      } catch {
        body = {};
      }

      const name = body?.name?.trim();
      const live_url = body?.live_url?.trim();
      res.setHeader('Content-Type', 'application/json; charset=utf-8');

      if (!name || !live_url) {
        res.statusCode = 400;
        res.end(JSON.stringify({ code: 400, message: '请填写完整信息', data: null }));
        return;
      }

      const duplicatedByUrl = sourceVideos.find(
        (video) => video.ownerId === CURRENT_USER_ID && video.live_url === live_url
      );
      if (duplicatedByUrl) {
        res.statusCode = 409;
        res.end(
          JSON.stringify({
            code: SOURCE_VIDEO_URL_DUPLICATE_CODE,
            message: '直播地址已存在',
            data: toPublicItem(duplicatedByUrl),
          })
        );
        return;
      }

      const now = nowIso();
      const item: MockSourceVideo = {
        id: Date.now(),
        name,
        live_url,
        remark: body.remark?.trim() || '',
        duration: 0,
        ext: '',
        ...createInitialAsrState(),
        created_at: now,
        updated_at: now,
        created_by: '管理员',
        ownerId: CURRENT_USER_ID,
      };

      sourceVideos.unshift(item);
      res.statusCode = 200;
      res.end(JSON.stringify({ code: 0, message: '', data: toPublicItem(item) }));
    },
  },
  {
    url: `${API_PREFIX}/v1/live-materials/:id`,
    method: 'get',
    response: ({ query }: { query: { id: string } }) => {
      tickAllAsrJobs();
      const item = sourceVideos.find(
        (video) => String(video.id) === query.id && video.ownerId === CURRENT_USER_ID
      );
      if (!item) {
        return { code: 404, message: '源视频不存在', data: null };
      }
      return { code: 0, message: '', data: toPublicDetail(item) };
    },
  },
  {
    url: `${API_PREFIX}/v1/live-materials/:id`,
    method: 'put',
    response: ({
      body,
      query,
    }: {
      body: { name?: string; remark?: string };
      query: { id: string };
    }) => {
      const item = sourceVideos.find(
        (video) => String(video.id) === query.id && video.ownerId === CURRENT_USER_ID
      );
      if (!item) {
        return { code: 404, message: '源视频不存在', data: null };
      }

      const name = body?.name?.trim();
      if (!name) {
        return { code: 400, message: '请填写源视频名称', data: null };
      }

      item.name = name;
      item.remark = body?.remark?.trim() || '';
      item.updated_at = nowIso();
      return { code: 0, message: '', data: toPublicItem(item) };
    },
  },
  {
    url: `${API_PREFIX}/v1/live-materials/:id`,
    method: 'delete',
    response: ({ query }: { query: { id: string } }) => {
      const index = sourceVideos.findIndex(
        (video) => String(video.id) === query.id && video.ownerId === CURRENT_USER_ID
      );
      if (index < 0) {
        return { code: 404, message: '源视频不存在', data: null };
      }
      sourceVideos.splice(index, 1);
      return { code: 0, message: '', data: null };
    },
  },
  {
    url: `${API_PREFIX}/v1/live-materials/:id/asr/retry`,
    method: 'post',
    response: ({ query }: { query: { id: string } }) => {
      const item = sourceVideos.find(
        (video) => String(video.id) === query.id && video.ownerId === CURRENT_USER_ID
      );
      if (!item) {
        return { code: 404, message: '源视频不存在', data: null };
      }
      if (item.asr_status !== 'failed') {
        return { code: 400, message: '当前 ASR 状态不可重新解析', data: null };
      }

      Object.assign(item, createInitialAsrState());
      item.updated_at = nowIso();

      return { code: 0, message: '', data: toPublicItem(item) };
    },
  },
  {
    url: `${API_PREFIX}/v1/live-materials/:id/asr/subtitle`,
    method: 'get',
    rawResponse: (req, res) => {
      const id = String(req.url?.match(/live-materials\/([^/]+)\/asr\/subtitle/)?.[1] ?? '');
      const item = sourceVideos.find(
        (video) => String(video.id) === id && video.ownerId === CURRENT_USER_ID
      );

      res.setHeader('Content-Type', 'application/json; charset=utf-8');

      if (!item) {
        res.statusCode = 404;
        res.end(JSON.stringify({ code: 404, message: '源视频不存在', data: null }));
        return;
      }

      if (item.asr_status !== 'completed') {
        res.statusCode = 400;
        res.end(
          JSON.stringify({
            code: 400,
            message: 'ASR 未完成，暂不可下载字幕',
            data: null,
          })
        );
        return;
      }

      const liveAsr = getTranscript(String(item.id));
      if (!liveAsr.length) {
        res.statusCode = 400;
        res.end(
          JSON.stringify({
            code: 400,
            message: '字幕内容为空，暂不可下载',
            data: null,
          })
        );
        return;
      }

      const filename = encodeURIComponent(`${item.name || 'subtitle'}-字幕.json`);
      res.statusCode = 200;
      res.setHeader('Content-Disposition', `attachment; filename*=UTF-8''${filename}`);
      res.end(JSON.stringify(liveAsr));
    },
  },
] as MockMethod[];
