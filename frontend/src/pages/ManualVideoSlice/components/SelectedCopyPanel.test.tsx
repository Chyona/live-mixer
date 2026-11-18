import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { cleanup, fireEvent, render, within } from '@testing-library/react';
import type { CopyInsertMode, SelectedCopySegment } from '../types';
import SelectedCopyPanel from './SelectedCopyPanel';

// jsdom 未实现 PointerEvent：用 MouseEvent 承载 pointer* 事件，才能传递 clientX/clientY。
if (typeof window.PointerEvent === 'undefined') {
  // @ts-expect-error 测试环境 polyfill
  window.PointerEvent = MouseEvent;
}

function makeSegment(id: string, index: number): SelectedCopySegment {
  return {
    id,
    speaker: '1',
    speakerName: '说话人1',
    text: id,
    start: index,
    end: index + 0.9,
  };
}

const segments = ['a', 'b', 'c', 'd'].map((id, index) => makeSegment(id, index));

function renderPanel(
  overrides: {
    activeSegmentId?: string | null;
    insertMode?: CopyInsertMode;
    onReorder?: (s: SelectedCopySegment[]) => void;
  } = {}
) {
  const props = {
    segments,
    paragraphs: [],
    aiSegments: [],
    currentTime: 0,
    activeSegmentId: overrides.activeSegmentId ?? null,
    insertMode: overrides.insertMode ?? ('timeline' as const),
    speakers: ['说话人1'],
    maxTotalDuration: 7200,
    videoDuration: 100,
    submitting: false,
    enableCaptions: false,
    onEnableCaptionsChange: vi.fn(),
    onActiveSegmentChange: vi.fn(),
    onSeek: vi.fn(),
    onReorder: overrides.onReorder ?? vi.fn(),
    onDeleteSegment: vi.fn(),
    onDeleteSelectedRange: vi.fn(),
    onCopySegment: vi.fn(),
    onAdjustSegment: vi.fn(),
    onClearAll: vi.fn(),
    onPreview: vi.fn(),
    onSave: vi.fn(),
    onSaveAs: vi.fn(),
    onExportDraft: vi.fn(),
    onSubmit: vi.fn(),
  };
  return { ...render(<SelectedCopyPanel {...props} />), props };
}

function copyItem(container: HTMLElement, id: string) {
  const element = container.querySelector<HTMLElement>(`[data-copy-id="${id}"]`);
  if (!element) throw new Error(`missing copy item ${id}`);
  return element;
}

function isMultiSelected(container: HTMLElement, id: string) {
  return copyItem(container, id).classList.contains('is-multi-selected');
}

describe('SelectedCopyPanel 多选', () => {
  it('Shift 点击选中锚点到当前片段的连续范围', () => {
    const { container, props } = renderPanel();

    fireEvent.click(copyItem(container, 'b'));
    fireEvent.click(copyItem(container, 'd'), { shiftKey: true });

    expect(isMultiSelected(container, 'b')).toBe(true);
    expect(isMultiSelected(container, 'c')).toBe(true);
    expect(isMultiSelected(container, 'd')).toBe(true);
    expect(isMultiSelected(container, 'a')).toBe(false);
    expect(props.onActiveSegmentChange).toHaveBeenLastCalledWith('d');
  });

  it('Ctrl/Cmd 点击切换单个片段，普通点击收敛为单选', () => {
    const { container } = renderPanel();

    fireEvent.click(copyItem(container, 'a'));
    fireEvent.click(copyItem(container, 'c'), { ctrlKey: true });
    expect(isMultiSelected(container, 'a')).toBe(true);
    expect(isMultiSelected(container, 'c')).toBe(true);

    fireEvent.click(copyItem(container, 'a'), { metaKey: true });
    expect(isMultiSelected(container, 'a')).toBe(false);
    expect(isMultiSelected(container, 'c')).toBe(false);

    fireEvent.click(copyItem(container, 'd'), { shiftKey: true });
    expect(isMultiSelected(container, 'c')).toBe(true);
    expect(isMultiSelected(container, 'd')).toBe(true);
  });

  it('普通点击只保留当前片段', () => {
    const { container } = renderPanel();

    fireEvent.click(copyItem(container, 'a'));
    fireEvent.click(copyItem(container, 'c'), { ctrlKey: true });
    expect(isMultiSelected(container, 'c')).toBe(true);

    fireEvent.click(copyItem(container, 'd'));
    expect(isMultiSelected(container, 'a')).toBe(false);
    expect(isMultiSelected(container, 'c')).toBe(false);
    // 单选不展示多选高亮，保持原来的 active 样式
    expect(isMultiSelected(container, 'd')).toBe(false);
  });
});

describe('SelectedCopyPanel 插入锚点', () => {
  it('跟随选中模式下，锚点显示在当前选中片段上；多选时取最后一段', () => {
    const { container } = renderPanel({ insertMode: 'after-selected' });

    fireEvent.click(copyItem(container, 'b'));
    expect(copyItem(container, 'b').classList.contains('is-insert-anchor')).toBe(true);
    expect(copyItem(container, 'a').classList.contains('is-insert-anchor')).toBe(false);

    fireEvent.click(copyItem(container, 'c'), { shiftKey: true });
    expect(copyItem(container, 'b').classList.contains('is-insert-anchor')).toBe(false);
    expect(copyItem(container, 'c').classList.contains('is-insert-anchor')).toBe(true);
    expect(
      within(copyItem(container, 'c')).getByText('新内容插入到此之后')
    ).toBeInTheDocument();
  });

  it('按时间模式下不显示插入锚点', () => {
    const { container } = renderPanel({ insertMode: 'timeline' });

    fireEvent.click(copyItem(container, 'b'));
    expect(copyItem(container, 'b').classList.contains('is-insert-anchor')).toBe(false);
    expect(container.textContent).not.toContain('新内容插入到此之后');
  });
});

describe('SelectedCopyPanel 多选拖动排序', () => {
  beforeEach(() => {
    const rectByIndex = (element: Element) => {
      const id = element.getAttribute('data-copy-id');
      const index = id ? segments.findIndex((segment) => segment.id === id) : 0;
      const top = index * 100;
      return {
        top,
        bottom: top + 100,
        height: 100,
        left: 0,
        right: 300,
        width: 300,
        x: 0,
        y: top,
        toJSON: () => ({}),
      } as DOMRect;
    };
    vi.spyOn(Element.prototype, 'getBoundingClientRect').mockImplementation(function (
      this: Element
    ) {
      return rectByIndex(this);
    });
    const proto = HTMLElement.prototype as HTMLElement & {
      setPointerCapture?: (pointerId: number) => void;
      releasePointerCapture?: (pointerId: number) => void;
      hasPointerCapture?: (pointerId: number) => boolean;
    };
    proto.setPointerCapture = vi.fn();
    proto.releasePointerCapture = vi.fn();
    proto.hasPointerCapture = vi.fn(() => true);
  });

  afterEach(() => {
    cleanup();
    vi.restoreAllMocks();
  });

  it('拖动多选片段时整组移动，并保持相对顺序', () => {
    const onReorder = vi.fn();
    const { container } = renderPanel({ onReorder });

    fireEvent.click(copyItem(container, 'a'));
    fireEvent.click(copyItem(container, 'b'), { shiftKey: true });

    const handle = copyItem(container, 'a').querySelector<HTMLElement>('.slice-editor-copy-drag');
    if (!handle) throw new Error('missing drag handle');

    fireEvent.pointerDown(handle, { clientX: 10, clientY: 50, pointerId: 1, button: 0 });
    // 指针移动到列表末尾，落点为最后一项之后
    fireEvent.pointerMove(handle, { clientX: 10, clientY: 350, pointerId: 1 });
    fireEvent.pointerUp(handle, { clientX: 10, clientY: 350, pointerId: 1 });

    expect(onReorder).toHaveBeenCalledTimes(1);
    const next = onReorder.mock.calls[0]?.[0] as SelectedCopySegment[];
    expect(next.map((segment) => segment.id)).toEqual(['c', 'd', 'a', 'b']);
  });

  it('拖动未选中的片段时只移动该片段', () => {
    const onReorder = vi.fn();
    const { container } = renderPanel({ onReorder });

    fireEvent.click(copyItem(container, 'a'));
    fireEvent.click(copyItem(container, 'b'), { shiftKey: true });

    const handle = copyItem(container, 'd').querySelector<HTMLElement>('.slice-editor-copy-drag');
    if (!handle) throw new Error('missing drag handle');

    fireEvent.pointerDown(handle, { clientX: 10, clientY: 350, pointerId: 1, button: 0 });
    // 移动到列表最前（落在 a 之前）
    fireEvent.pointerMove(handle, { clientX: 10, clientY: 10, pointerId: 1 });
    fireEvent.pointerUp(handle, { clientX: 10, clientY: 10, pointerId: 1 });

    expect(onReorder).toHaveBeenCalledTimes(1);
    const next = onReorder.mock.calls[0]?.[0] as SelectedCopySegment[];
    expect(next.map((segment) => segment.id)).toEqual(['d', 'a', 'b', 'c']);
  });
});
