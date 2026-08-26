import { describe, expect, it } from 'vitest';
import type { SelectedCopySegment } from '~/pages/ManualVideoSlice/types';
import {
  serializeManualSliceProjectState,
  serializeTimelineSliceProjectState,
} from './sliceProjectDirty';

function makeSegment(
  overrides: Partial<SelectedCopySegment> & Pick<SelectedCopySegment, 'start' | 'end' | 'text'>
): SelectedCopySegment {
  return {
    id: 'seg-1',
    speaker: 'speaker-1',
    speakerName: 'Speaker 1',
    ...overrides,
  };
}

describe('serializeManualSliceProjectState', () => {
  const base = {
    segments: [makeSegment({ start: 1.2, end: 3.4, text: 'hello' })],
    enableCaptions: false,
    draftName: '  项目 A  ',
    projectRemark: '  备注  ',
  };

  it('相同内容序列化结果一致', () => {
    const snapshot = serializeManualSliceProjectState(base);
    expect(serializeManualSliceProjectState({ ...base })).toBe(snapshot);
  });

  it('片段变更后序列化结果不同', () => {
    const snapshot = serializeManualSliceProjectState(base);
    expect(
      serializeManualSliceProjectState({
        ...base,
        segments: [...base.segments, makeSegment({ id: 'seg-2', start: 5, end: 8, text: 'world' })],
      })
    ).not.toBe(snapshot);
  });

  it('会 trim 项目名称与备注后再比较', () => {
    const snapshot = serializeManualSliceProjectState(base);
    expect(
      serializeManualSliceProjectState({
        ...base,
        draftName: '项目 A',
        projectRemark: '备注',
      })
    ).toBe(snapshot);
  });
});

describe('serializeTimelineSliceProjectState', () => {
  const base = {
    ranges: [{ id: 'a', start: 10.2, end: 20.6, title: '  片段  ' }],
    promptId: 3,
    projectName: '  时间轴项目  ',
  };

  it('相同内容序列化结果一致', () => {
    const snapshot = serializeTimelineSliceProjectState(base);
    expect(serializeTimelineSliceProjectState({ ...base })).toBe(snapshot);
  });

  it('选区变更后序列化结果不同', () => {
    const snapshot = serializeTimelineSliceProjectState(base);
    expect(
      serializeTimelineSliceProjectState({
        ...base,
        ranges: [{ id: 'b', start: 10.2, end: 25, title: '片段' }],
      })
    ).not.toBe(snapshot);
  });

  it('会 trim 项目名称后再比较', () => {
    const snapshot = serializeTimelineSliceProjectState(base);
    expect(
      serializeTimelineSliceProjectState({
        ...base,
        projectName: '时间轴项目',
      })
    ).toBe(snapshot);
  });
});
