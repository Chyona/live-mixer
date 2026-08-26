import type { TimeRange } from '~/components/VideoTimeline';
import type { SelectedCopySegment } from '~/pages/ManualVideoSlice/types';
import { toSliceProjectClips } from '~/services/sliceProject';

export function serializeManualSliceProjectState(input: {
  segments: SelectedCopySegment[];
  enableCaptions: boolean;
  draftName: string;
  projectRemark: string;
}): string {
  return JSON.stringify({
    clips1: toSliceProjectClips(input.segments),
    enableCaptions: Boolean(input.enableCaptions),
    name: input.draftName.trim(),
    remark: input.projectRemark.trim(),
  });
}

function serializeTimelineClips(ranges: TimeRange[]) {
  return [...ranges]
    .map((range) => ({
      start_time: Math.round(range.start * 1000),
      end_time: Math.round(range.end * 1000),
      ...(range.title?.trim() ? { title: range.title.trim() } : {}),
    }))
    .sort((a, b) => a.start_time - b.start_time || a.end_time - b.end_time);
}

export function serializeTimelineSliceProjectState(input: {
  ranges: TimeRange[];
  promptId: number | null;
  projectName: string;
}): string {
  return JSON.stringify({
    clips0: serializeTimelineClips(input.ranges),
    promptId: input.promptId ?? 0,
    name: input.projectName.trim(),
  });
}
