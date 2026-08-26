/** 列表搜索可识别为「多关键词分隔」的字符（含常见中文/全角分隔线） */
const KEYWORD_SEPARATOR_RE = /[,，+＋|｜丨、；;/／\\\s]+/;

/** OR / AND 语义分隔（用于拆词高亮） */
const KEYWORD_OR_AND_SEPARATOR_RE = /[|｜丨+＋]/;

/**
 * 将中文/全角分隔符统一为对应英文字符，保留原有分隔语义。
 * 例如：「财富｜格式」→「财富|格式」，「A＋B」→「A+B」
 */
export function normalizeKeywordSeparators(input: string): string {
  return input
    .replace(/[｜丨|¦]/g, '|')
    .replace(/[，、]/g, ',')
    .replace(/[＋]/g, '+')
    .replace(/[；]/g, ';')
    .replace(/[／]/g, '/')
    .replace(/\u3000/g, ' ');
}

/**
 * 解析列表搜索关键词：支持「A+B」「A B」「A,B」「A|B」「A｜B」等分隔。
 */
export function parseListKeywords(input?: string | null): string[] {
  if (!input?.trim()) return [];
  const normalized = normalizeKeywordSeparators(input.trim());
  return normalized
    .split(KEYWORD_SEPARATOR_RE)
    .map((part) => part.trim())
    .filter(Boolean);
}

/**
 * 解析用于命中文案高亮的关键词（A OR B / A AND B 均拆成多个词）。
 */
export function parseHighlightKeywords(input?: string | null): string[] {
  if (!input?.trim()) return [];
  const normalized = normalizeKeywordSeparators(input.trim());

  if (KEYWORD_OR_AND_SEPARATOR_RE.test(normalized)) {
    const orAndParts = normalized
      .split(KEYWORD_OR_AND_SEPARATOR_RE)
      .map((part) => part.trim())
      .filter(Boolean);
    if (orAndParts.length > 1) {
      return orAndParts;
    }
  }

  return parseListKeywords(normalized);
}

/**
 * 多关键词全部命中（AND）。
 */
export function matchListKeywords(text: string, keywords: string[]): boolean {
  if (!keywords.length) return true;
  const lower = text.toLowerCase();
  return keywords.every((keyword) => lower.includes(keyword.toLowerCase()));
}

/** 任一关键词命中（OR），用于筛选命中语句 */
export function matchAnyListKeyword(text: string, keywords: string[]): boolean {
  if (!keywords.length) return false;
  const lower = text.toLowerCase();
  return keywords.some((keyword) => lower.includes(keyword.toLowerCase()));
}

/**
 * 列表搜索关键词规范化：中文分隔符转英文后直接传给后端（不改为逗号拼接）。
 */
export function toApiKeywords(input?: string | null): string | undefined {
  const trimmed = input?.trim();
  if (!trimmed) return undefined;
  return normalizeKeywordSeparators(trimmed);
}

function escapeHtml(text: string) {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

function escapeRegExp(text: string) {
  return text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
}

function resolveHighlightTerms(keywords: string[], rawInput?: string | null): string[] {
  let terms = keywords.map((item) => item.trim()).filter(Boolean);

  if (!terms.length && rawInput?.trim()) {
    terms = parseHighlightKeywords(rawInput);
  }

  if (terms.length === 1 && KEYWORD_OR_AND_SEPARATOR_RE.test(terms[0]!)) {
    terms = parseHighlightKeywords(terms[0]);
  }

  return [...new Set(terms)].sort((a, b) => b.length - a.length);
}

/** 生成用于 dangerouslySetInnerHTML 的多关键词高亮 HTML */
export function highlightListKeywords(
  text: string,
  keywords: string[],
  rawInput?: string | null
): string {
  const safeText = escapeHtml(text);
  const terms = resolveHighlightTerms(keywords, rawInput);
  if (!terms.length) return safeText;

  const parts = terms.map((keyword) => escapeRegExp(escapeHtml(keyword))).filter(Boolean);
  if (!parts.length) return safeText;

  const regex = new RegExp(`(${parts.join('|')})`, 'gi');
  return safeText.replace(regex, '<span class="list-keyword-hit">$1</span>');
}

/** 单一关键词字面量高亮（不解析 | + 等分隔符） */
export function highlightPlainKeyword(text: string, keyword: string): string {
  const trimmed = keyword.trim();
  const safeText = escapeHtml(text);
  if (!trimmed) return safeText;

  const safeKeyword = escapeRegExp(escapeHtml(trimmed));
  const regex = new RegExp(`(${safeKeyword})`, 'gi');
  return safeText.replace(regex, '<span class="list-keyword-hit">$1</span>');
}
