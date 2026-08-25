import type { MockMethod } from 'vite-plugin-mock';
import { matchListKeywords, parseListKeywords } from '../src/utils/listKeywords';
import { API_PREFIX } from './_config';

type MockSliceItem = {
  id: string;
  name: string;
  startTime: number;
  endTime: number;
  duration: number;
  date: string;
  sourceVideoId: string;
  sourceVideoName: string;
  sourceVideoRemarkName: string;
  liveUrl: string;
  previewUrl: string;
  auditStatus: 'approved' | 'rejected' | 'pending';
  ownerId: string;
};

const CURRENT_USER_ID = '222';

const SLICE_NAME_TEMPLATES = [
  '精彩进球瞬间',
  '开场介绍片段',
  '用户互动高光',
  '产品讲解重点',
  '结尾总结回顾',
  '搞笑翻车合集',
  '弹幕热梗剪辑',
  '主播回应片段',
  '操作教学示范',
  '剧情反转时刻',
];

const SOURCE_VIDEO_TEMPLATES = [
  {
    id: 'sv-001',
    name: '周末游戏直播回放',
    remarkName: '游戏专场素材',
    liveUrl: 'rtmp://live.example.com/stream/001',
  },
  {
    id: 'sv-002',
    name: '新品发布会直播',
    remarkName: '发布会实录',
    liveUrl: 'https://live.example.com/hls/product_launch.m3u8',
  },
  {
    id: 'sv-003',
    name: '户外探店直播',
    remarkName: '探店精选',
    liveUrl: 'rtmp://live.example.com/stream/003',
  },
  {
    id: 'sv-004',
    name: '美食烹饪教学',
    remarkName: '厨房直播',
    liveUrl: 'https://live.example.com/hls/cooking.m3u8',
  },
  {
    id: 'sv-005',
    name: '数码产品测评',
    remarkName: '测评专场',
    liveUrl: 'rtmp://live.example.com/stream/005',
  },
];

const MOCK_PREVIEW_URL =
  'https://commondatastorage.googleapis.com/gtv-videos-bucket/sample/BigBuckBunny.mp4';

const AUDIT_STATUSES: MockSliceItem['auditStatus'][] = ['approved', 'rejected', 'pending'];

function buildMockSlices(): MockSliceItem[] {
  return Array.from({ length: 20 }, (_, index) => {
    const id = String(index + 1).padStart(3, '0');
    const day = String((index % 28) + 1).padStart(2, '0');
    const source = SOURCE_VIDEO_TEMPLATES[index % SOURCE_VIDEO_TEMPLATES.length];
    const auditStatus = AUDIT_STATUSES[index % AUDIT_STATUSES.length];
    const startTime = 120 * (index + 1);
    const endTime = startTime + 45 + (index % 8) * 37;
    const duration = endTime - startTime;

    return {
      id: `slice-${id}`,
      name: `${SLICE_NAME_TEMPLATES[index % SLICE_NAME_TEMPLATES.length]} ${index + 1}`,
      startTime,
      endTime,
      duration,
      date: `2026-06-${day}`,
      sourceVideoId: source.id,
      sourceVideoName: source.name,
      sourceVideoRemarkName: source.remarkName,
      liveUrl: source.liveUrl,
      previewUrl: MOCK_PREVIEW_URL,
      auditStatus,
      ownerId: CURRENT_USER_ID,
    };
  });
}

const slices: MockSliceItem[] = [
  ...buildMockSlices(),
  {
    id: 'slice-999',
    name: '其他用户切片',
    startTime: 300,
    endTime: 360,
    duration: 60,
    date: '2026-07-01',
    sourceVideoId: 'sv-999',
    sourceVideoName: '其他用户直播素材',
    sourceVideoRemarkName: '不应展示',
    liveUrl: 'rtmp://live.example.com/stream/other',
    previewUrl: MOCK_PREVIEW_URL,
    auditStatus: 'approved',
    ownerId: 'other-user',
  },
];

function toPublicItem(item: MockSliceItem) {
  const { ownerId: _ownerId, ...rest } = item;
  return rest;
}

function filterList(query: Record<string, string | string[] | undefined>) {
  const date = typeof query.date === 'string' ? query.date : undefined;
  const dateEnd = typeof query.dateEnd === 'string' ? query.dateEnd : undefined;
  const keyword = typeof query.keyword === 'string' ? query.keyword : undefined;
  const titleKeywords = parseListKeywords(keyword);

  return slices.filter((item) => {
    if (item.ownerId !== CURRENT_USER_ID) return false;

    if (date && item.date < date) return false;
    if (dateEnd && item.date > dateEnd) return false;

    const titleText = `${item.sourceVideoName} ${item.sourceVideoRemarkName}`;
    if (!matchListKeywords(titleText, titleKeywords)) return false;

    return true;
  });
}

export default [
  {
    url: `${API_PREFIX}/v1/slices`,
    method: 'get',
    response: ({ query }: { query: Record<string, string | string[] | undefined> }) => {
      const page = Number(query.page || 1);
      const pageSize = Number(query.pageSize || 10);
      const filtered = filterList(query);
      const start = (page - 1) * pageSize;

      return {
        code: 0,
        message: '',
        data: {
          list: filtered.slice(start, start + pageSize).map(toPublicItem),
          total: filtered.length,
        },
      };
    },
  },
  {
    url: `${API_PREFIX}/v1/slices/:id/name`,
    method: 'put',
    response: ({ body, query }: { body: { name?: string }; query: { id: string } }) => {
      const item = slices.find((slice) => slice.id === query.id && slice.ownerId === CURRENT_USER_ID);
      if (!item) {
        return { code: 404, message: '切片不存在', data: null };
      }

      item.name = body?.name?.trim() || '';
      return { code: 0, message: '', data: toPublicItem(item) };
    },
  },
] as MockMethod[];
