const KEYWORD_SEPARATOR_RE = /[,，+＋\s]+/;

/**
 * 解析列表搜索关键词：支持「A+B」「A B」「A,B」等分隔。
 */
export function parseListKeywords(input?: string | null): string[] {
  if (!input?.trim()) return [];
  return input
    .split(KEYWORD_SEPARATOR_RE)
    .map((part) => part.trim())
    .filter(Boolean);
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
 * 列表搜索关键词规范化：统一为逗号分隔后传给后端。
 */
export function toApiKeywords(input?: string | null): string | undefined {
  const keywords = parseListKeywords(input);
  return keywords.length ? keywords.join(',') : undefined;
}

function escapeHtml(text: string) {
  return text
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}

/** 生成用于 dangerouslySetInnerHTML 的多关键词高亮 HTML */
export function highlightListKeywords(text: string, keywords: string[]): string {
  const safeText = escapeHtml(text);
  if (!keywords.length) return safeText;

  const parts = keywords
    .map((keyword) => escapeHtml(keyword).replace(/[.*+?^${}()|[\]\\]/g, '\\$&'))
    .filter(Boolean);
  if (!parts.length) return safeText;

  const regex = new RegExp(`(${parts.join('|')})`, 'gi');
  return safeText.replace(regex, '<mark>$1</mark>');
}
