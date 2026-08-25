import type { MockMethod } from 'vite-plugin-mock';
import { matchListKeywords, parseListKeywords } from '../src/utils/listKeywords';
import { API_PREFIX } from './_config';

type MockAiPrompt = {
  id: number;
  name: string;
  content: string;
  remark: string;
  created_by: string;
  created_at: string;
  updated_at: string;
  is_editable: number;
};

const CURRENT_USER_NAME = '管理员';
const OTHER_USER_NAME = '其他用户';

const PROMPT_TEMPLATES = [
  { name: '同商品连贯性', content: '确保生成的视频整体都是在讲同一个商品确保生成的视频整体都是在讲同一个商品确保生成的视频整体都是在讲同一个商品确保生成的视频整体都是在讲同一个商品确保生成的视频整体都是在讲同一个商品', remark: '时间轴切片默认提示词' },
  { name: '人工文案成片', content: '根据人工选择的文案片段生成成片', remark: '人工切片场景' },
  { name: '高光集锦', content: '提取直播中最有感染力和转化力的片段，生成 30 秒以内的短视频', remark: '' },
  { name: '开场引流', content: '保留直播开场 30 秒内最具吸引力的内容，突出主题与福利信息', remark: '' },
  { name: '产品卖点提炼', content: '围绕单一商品提取核心卖点，去除无关闲聊与重复话术', remark: '' },
  { name: '价格促销强调', content: '聚焦限时优惠、赠品与下单引导，形成高转化短视频', remark: '' },
  { name: '用户互动精选', content: '保留主播与观众互动中最有共鸣的问答片段', remark: '' },
  { name: '场景氛围感', content: '突出画面氛围与情绪表达，适合旅游、探店类内容', remark: '' },
  { name: '教程步骤精简', content: '按步骤提取教学重点，保证逻辑连贯、节奏紧凑', remark: '' },
  { name: '对比测评', content: '保留产品对比与实测结论，突出差异与推荐理由', remark: '' },
  { name: '故事化表达', content: '以故事线串联片段，让成片更具观看连续性', remark: '' },
  { name: '口播金句', content: '提取主播口播中最有记忆点的金句与观点', remark: '' },
  { name: '多片段串联', content: '将多个相关片段按时间顺序串联，形成完整叙事', remark: '' },
  { name: '竖屏短视频', content: '优先保留人物居中、画面稳定的片段，适配竖屏发布', remark: '' },
  { name: '品牌调性', content: '保持语言风格与品牌调性一致，避免过于口语化的冗余表达', remark: '' },
  { name: '结尾引导', content: '保留直播结尾的总结、福利提醒与关注引导', remark: '' },
  { name: '痛点共鸣', content: '围绕用户痛点展开，突出问题与解决方案', remark: '' },
  { name: '节奏加速', content: '去除停顿与无效片段，让成片节奏更快、信息密度更高', remark: '' },
  { name: '字幕友好', content: '优先选择口播清晰、背景噪音较少的片段', remark: '' },
  { name: '本地生活探店', content: '突出门店环境、特色菜品与消费体验，适合本地生活推广', remark: '' },
  { name: '游戏高光', content: '提取游戏直播中的精彩操作与反转瞬间', remark: '' },
  { name: '母婴种草', content: '围绕母婴产品使用场景与真实体验进行内容提炼', remark: '' },
];

function buildMockAiPrompts(): MockAiPrompt[] {
  return PROMPT_TEMPLATES.map((item, index) => {
    const id = index + 1;
    const day = String((index % 28) + 1).padStart(2, '0');
    const timestamp = `2026-06-${day}T10:00:00.000000+08:00`;

    return {
      id,
      name: item.name,
      content: item.content,
      remark: item.remark,
      created_by: CURRENT_USER_NAME,
      created_at: timestamp,
      updated_at: timestamp,
      is_editable: 1,
    };
  });
}

const aiPrompts: MockAiPrompt[] = [
  ...buildMockAiPrompts(),
  {
    id: 999,
    name: '其他用户提示词',
    content: '不应展示',
    remark: '',
    created_by: OTHER_USER_NAME,
    created_at: '2026-06-01T10:00:00.000000+08:00',
    updated_at: '2026-06-01T10:00:00.000000+08:00',
    is_editable: 0,
  },
];

let nextId = 1000;

function toPublicItem(item: MockAiPrompt) {
  return item;
}

function formatNow() {
  return new Date().toISOString().replace('Z', '+08:00');
}

function parsePromptId(id: string | number | undefined) {
  const numericId = Number(id);
  return Number.isFinite(numericId) ? numericId : NaN;
}

function findOwnedPrompt(id: string | number | undefined) {
  const numericId = parsePromptId(id);
  if (!Number.isFinite(numericId)) return undefined;
  return aiPrompts.find((prompt) => prompt.id === numericId && prompt.created_by === CURRENT_USER_NAME);
}

function filterList(query: Record<string, string | string[] | undefined>) {
  const keywordsParam = typeof query.keywords === 'string' ? query.keywords : undefined;
  const keywords = parseListKeywords(keywordsParam);
  const startDate = typeof query.start_date === 'string' ? query.start_date : undefined;
  const endDate = typeof query.end_date === 'string' ? query.end_date : undefined;

  return aiPrompts.filter((item) => {
    if (item.created_by !== CURRENT_USER_NAME) return false;

    const createdDate = item.created_at.slice(0, 10);
    if (startDate && createdDate < startDate) return false;
    if (endDate && createdDate > endDate) return false;

    const searchText = `${item.name} ${item.content} ${item.remark}`;
    return matchListKeywords(searchText, keywords);
  });
}

export default [
  {
    url: `${API_PREFIX}/v1/llm-system-prompts`,
    method: 'get',
    response: ({ query }: { query: Record<string, string | string[] | undefined> }) => {
      const page = Number(query.page || 1);
      const pageSize = Number(query.page_size || 10);
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
    url: `${API_PREFIX}/v1/llm-system-prompts`,
    method: 'post',
    response: ({
      body,
    }: {
      body: {
        name?: string;
        content?: string;
        remark?: string;
      };
    }) => {
      const name = body?.name?.trim();
      const content = body?.content?.trim();

      if (!name || !content) {
        return { code: 400, message: '请填写名称和提示词信息', data: null };
      }

      const now = formatNow();
      const item: MockAiPrompt = {
        id: nextId++,
        name,
        content,
        remark: body.remark?.trim() || '',
        created_by: CURRENT_USER_NAME,
        created_at: now,
        updated_at: now,
        is_editable: 1,
      };

      aiPrompts.unshift(item);

      return { code: 0, message: '', data: toPublicItem(item) };
    },
  },
  {
    url: `${API_PREFIX}/v1/llm-system-prompts/:id`,
    method: 'put',
    response: ({
      body,
      query,
    }: {
      body: {
        name?: string;
        content?: string;
        remark?: string;
      };
      query: { id: string };
    }) => {
      const item = findOwnedPrompt(query.id);
      if (!item) {
        return { code: 404, message: '提示词不存在', data: null };
      }

      if (item.is_editable !== 1) {
        return { code: 403, message: '该提示词不可编辑', data: null };
      }

      const name = body?.name?.trim();
      const content = body?.content?.trim();
      if (!name || !content) {
        return { code: 400, message: '请填写名称和提示词信息', data: null };
      }

      item.name = name;
      item.content = content;
      item.remark = body.remark?.trim() || '';
      item.updated_at = formatNow();

      return { code: 0, message: '', data: toPublicItem(item) };
    },
  },
  {
    url: `${API_PREFIX}/v1/llm-system-prompts/:id`,
    method: 'delete',
    response: ({ query }: { query: { id: string } }) => {
      const item = findOwnedPrompt(query.id);
      if (!item) {
        return { code: 404, message: '提示词不存在', data: null };
      }

      if (item.is_editable !== 1) {
        return { code: 403, message: '该提示词不可删除', data: null };
      }

      const index = aiPrompts.findIndex((prompt) => prompt.id === item.id);
      aiPrompts.splice(index, 1);
      return { code: 0, message: '', data: null };
    },
  },
] as MockMethod[];
