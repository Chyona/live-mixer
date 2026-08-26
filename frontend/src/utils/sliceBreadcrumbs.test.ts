import { describe, expect, it } from 'vitest';
import {
  UNSAVED_SLICE_PROJECT_TITLE,
  buildSliceBreadcrumbItems,
  resolveSlicePageTitle,
} from './sliceBreadcrumbs';

describe('resolveSlicePageTitle', () => {
  it('已保存项目显示项目名称', () => {
    expect(
      resolveSlicePageTitle({
        saved: true,
        projectName: '88财富节直播剪辑',
      })
    ).toBe('88财富节直播剪辑');
  });

  it('新建未保存显示未命名项目', () => {
    expect(
      resolveSlicePageTitle({
        saved: false,
        projectName: '人工切片_2026-01-01',
      })
    ).toBe(UNSAVED_SLICE_PROJECT_TITLE);
  });

  it('已保存但项目名为空时回退未命名项目', () => {
    expect(resolveSlicePageTitle({ saved: true, projectName: '  ' })).toBe(UNSAVED_SLICE_PROJECT_TITLE);
  });
});

describe('buildSliceBreadcrumbItems', () => {
  it('已保存项目面包屑末级只显示模式名', () => {
    const items = buildSliceBreadcrumbItems({
      entryFrom: 'slices',
      sourceVideoId: '10',
      pageKind: 'manual',
      videoName: '源视频A',
      projectName: '巴菲特VS孙正义',
      saved: true,
    });

    expect(items).toHaveLength(2);
    expect(items?.[1]?.title).toBe('人工切片');
  });

  it('未保存项目面包屑末级显示源视频名', () => {
    const items = buildSliceBreadcrumbItems({
      entryFrom: 'source-videos',
      sourceVideoId: '10',
      pageKind: 'manual',
      videoName: '源视频A',
      saved: false,
    });

    expect(items?.[1]?.title).toBe('源视频A - 人工切片');
  });
});
