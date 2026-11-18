import { beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, within } from '@testing-library/react';
import type { TranscriptParagraph } from '../types';
import type { SelectedCopyHighlightRange } from '../utils';
import TranscriptPanel from './TranscriptPanel';

// jsdom 缺少列表测量与选区能力，补最小实现。
class ResizeObserverMock {
  observe() {}
  unobserve() {}
  disconnect() {}
}
if (!('ResizeObserver' in globalThis)) {
  (globalThis as unknown as { ResizeObserver: unknown }).ResizeObserver = ResizeObserverMock;
}
if (!window.getSelection) {
  Object.defineProperty(window, 'getSelection', {
    value: () => ({
      isCollapsed: true,
      removeAllRanges() {},
    }),
  });
}
// jsdom 在无 origin 配置时没有 localStorage；组件用它记忆「定位跟随」开关。
if (!globalThis.localStorage) {
  const store = new Map<string, string>();
  const storage = {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => {
      store.set(key, String(value));
    },
    removeItem: (key: string) => {
      store.delete(key);
    },
    clear: () => {
      store.clear();
    },
    key: (index: number) => [...store.keys()][index] ?? null,
    get length() {
      return store.size;
    },
  };
  Object.defineProperty(globalThis, 'localStorage', { value: storage, configurable: true });
}

const paragraphs: TranscriptParagraph[] = [
  {
    id: 'p1',
    speaker: '1',
    speakerName: '说话人1',
    segments: [{ id: 's1', start: 0, end: 2, text: '第一段文案' }],
  },
  {
    id: 'p2',
    speaker: '1',
    speakerName: '说话人1',
    segments: [{ id: 's2', start: 2, end: 4, text: '第二段文案' }],
  },
];

function renderPanel(selectedCopyHighlights: SelectedCopyHighlightRange[] = []) {
  const props = {
    paragraphs,
    keyword: '',
    onKeywordChange: vi.fn(),
    onPrevMatch: vi.fn(),
    onNextMatch: vi.fn(),
    activeParagraphId: null,
    transcriptHighlight: null,
    selectedCopyHighlights,
    activeMatchIndex: 0,
    matchParagraphIds: [],
    onSeek: vi.fn(),
    onSelectSegment: vi.fn(),
    onLocateParagraph: vi.fn(),
    insertMode: 'timeline' as const,
    onInsertModeChange: vi.fn(),
  };
  return { ...render(<TranscriptPanel {...props} />), props };
}

function paragraphElement(container: HTMLElement, id: string) {
  const element = container.querySelector<HTMLElement>(`[data-paragraph-id="${id}"]`);
  if (!element) throw new Error(`missing paragraph ${id}`);
  return element;
}

describe('TranscriptPanel 选中与加入预览', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('单击段落显示明显的选中标记，且只保留一个', () => {
    const { container } = renderPanel();

    fireEvent.click(paragraphElement(container, 'p1'));
    expect(paragraphElement(container, 'p1').classList.contains('selected')).toBe(true);

    fireEvent.click(paragraphElement(container, 'p2'));
    expect(paragraphElement(container, 'p1').classList.contains('selected')).toBe(false);
    expect(paragraphElement(container, 'p2').classList.contains('selected')).toBe(true);
  });

  it('双击整段加入预览，并保持该段选中', () => {
    const { container, props } = renderPanel();

    fireEvent.doubleClick(paragraphElement(container, 'p1'));

    expect(props.onSelectSegment).toHaveBeenCalledTimes(1);
    expect(props.onSelectSegment.mock.calls[0]?.[0]?.text).toBe('第一段文案');
    expect(paragraphElement(container, 'p1').classList.contains('selected')).toBe(true);
  });

  it('点击「加入预览」按钮只加入一次，不触发段落定位', () => {
    const { container, props } = renderPanel();

    const button = within(paragraphElement(container, 'p1')).getByRole('button', {
      name: /加入预览/,
    });
    fireEvent.click(button);

    expect(props.onSelectSegment).toHaveBeenCalledTimes(1);
    expect(props.onSeek).not.toHaveBeenCalled();
    expect(paragraphElement(container, 'p1').classList.contains('selected')).toBe(true);
  });

  it('可切换插入方式，默认按时间', () => {
    const { container, props } = renderPanel();
    const modeScope = within(
      container.querySelector<HTMLElement>('.slice-editor-transcript-insert-mode')!
    );

    expect(modeScope.getByText('按时间')).toBeInTheDocument();
    fireEvent.click(modeScope.getByText('跟随选中'));
    expect(props.onInsertModeChange).toHaveBeenCalledWith('after-selected');
  });

  it('已加入右侧预览的段落显示「已添加」标记', () => {
    const { container } = renderPanel([
      { paragraphId: 'p1', charStart: 0, charEnd: 2, segmentId: 'c1' },
      { paragraphId: 'p1', charStart: 3, charEnd: 5, segmentId: 'c2' },
    ]);

    expect(within(paragraphElement(container, 'p1')).getByText('已添加 2')).toBeInTheDocument();
    expect(
      within(paragraphElement(container, 'p2')).queryByText(/已添加/)
    ).not.toBeInTheDocument();
  });
});
