import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { Button, Descriptions, Drawer, Tooltip, Typography } from 'antd';
import { LuX } from 'react-icons/lu';
import VideoTimeline, { type TimeRange } from '~/components/VideoTimeline';
import SliceVideoPlayer from '~/components/SliceVideoPlayer';
import type { StreamVideoPlayerHandle } from '~/components/StreamVideoPlayer';
import SlicePageHeader, { SliceProjectMetaBar } from '~/components/SlicePageHeader';
import SlicePageEmptyState from '~/components/SlicePageEmptyState';
import CopyableText from '~/components/CopyableText';
import { useAppSEO } from '~/hooks/useAppSEO';
import { useSliceProjectTaskStats } from '~/hooks/useSliceProjectTaskStats';
import {
  SliceProjectTaskReadOnlyAlert,
} from '~/components/SliceProjectTaskStatus';
import { AppError } from '~/services/http';
import { fetchSourceVideoDetail, type SourceVideo } from '~/services/sourceVideo';
import { submitClip } from '~/services/slice';
import { submitAiSliceSelection } from '~/services/aiSlice';
import { type AiPrompt } from '~/services/aiPrompt';
import {
  fetchSliceProjectDetail,
  updateSliceProject,
  type SliceProjectClip,
} from '~/services/sliceProject';
import { showAppError, toast } from '~/utils/toast';
import { formatToDateTime } from '~/utils/date';
import { formatVideoDurationMs } from '~/utils/duration';
import { useSliceEntryFrom } from '~/hooks/useSliceEntryFrom';
import { useSliceProjectLeaveGuard } from '~/context/SliceLeaveGuardContext';
import {
  buildManualVideoSliceLink,
  buildSourceVideoSliceLink,
  parseProjectId,
} from '~/routes/links';
import { buildSliceBreadcrumbItems, resolveSlicePageTitle } from '~/utils/sliceBreadcrumbs';
import { serializeTimelineSliceProjectState } from '~/utils/sliceProjectDirty';
import { getVideoFormatLabel, isPlayableVideoUrl } from '~/utils/videoUrl';
import SelectedSegmentsPanel from './SelectedSegmentsPanel';
import SourceVideoSlicePageSkeleton from './SourceVideoSlicePageSkeleton';
import TimelineLoadingSkeleton from './TimelineLoadingSkeleton';
import PromptPickerPanel from './PromptPickerPanel';
import {
  asrParagraphsToTranscriptParagraphs,
  normalizeTranscriptParagraphs,
} from '../ManualVideoSlice/utils';
import type { TranscriptParagraph } from '../ManualVideoSlice/types';

const MIN_TOTAL_DURATION = 5 * 60;

function clips0ToTimeRanges(clips: SliceProjectClip[] | undefined): TimeRange[] {
  if (!clips?.length) return [];
  return clips.map((clip, index) => {
    const start = (clip.start_time ?? 0) / 1000;
    const end = (clip.end_time ?? 0) / 1000;
    return {
      id: `timeline-${index}-${Math.round(start * 1000)}-${Math.round(end * 1000)}`,
      start,
      end,
    };
  });
}

function asrSummariesToTimeRanges(
  summaries: SourceVideo['asr_summaries'] | undefined
): TimeRange[] {
  if (!summaries?.length) return [];
  return summaries.map((item, index) => {
    const start = (item.start_time ?? 0) / 1000;
    const end = (item.end_time ?? 0) / 1000;
    const title = item.title?.trim() || undefined;
    return {
      id: `summary-${index}-${Math.round(start * 1000)}-${Math.round(end * 1000)}`,
      start,
      end,
      title,
    };
  });
}

/** 优先 clips0 作为已选片段；asr_summaries 仅作 AI 预选展示 */
function resolveSelectedRanges(clips0: SliceProjectClip[] | undefined): TimeRange[] {
  return clips0ToTimeRanges(clips0);
}

/** 时间轴选区 → 接口 clips0（直接传 title） */
function selectedRangesToClips0(ranges: TimeRange[]): SliceProjectClip[] {
  return ranges.map((range) => {
    const title = range.title?.trim();
    return {
      start_time: Math.round(range.start * 1000),
      end_time: Math.round(range.end * 1000),
      ...(title ? { title } : {}),
    };
  });
}

const SourceVideoSlicePage = () => {
  const { id: sourceVideoId = '' } = useParams();
  const [searchParams] = useSearchParams();
  const projectId = parseProjectId(searchParams.get('projectId'));
  const navigate = useNavigate();
  const entryFrom = useSliceEntryFrom();
  const {
    runningTaskCount,
    readOnly: projectTaskReadOnly,
  } = useSliceProjectTaskStats(projectId);
  const [loading, setLoading] = useState(true);
  const [video, setVideo] = useState<SourceVideo | null>(null);
  const [projectName, setProjectName] = useState('');
  const [projectTitle, setProjectTitle] = useState('');
  const [projectDescription, setProjectDescription] = useState('');
  const [projectTopics, setProjectTopics] = useState<string[]>([]);
  const [sourceModalVisible, setSourceModalVisible] = useState(false);
  const [videoDuration, setVideoDuration] = useState(0);
  const [currentTime, setCurrentTime] = useState(0);
  const [paragraphs, setParagraphs] = useState<TranscriptParagraph[]>([]);
  const [selectedRanges, setSelectedRanges] = useState<TimeRange[]>([]);
  const [submitting, setSubmitting] = useState(false);
  const [aiSelecting, setAiSelecting] = useState(false);
  const [autoPlayOnSelect, setAutoPlayOnSelect] = useState(true);
  const [timelineZoomLevel, setTimelineZoomLevel] = useState(1);
  const [activeRangeId, setActiveRangeId] = useState<string | null>(null);
  const [videoError, setVideoError] = useState<string | null>(null);
  const [selectedPrompt, setSelectedPrompt] = useState<AiPrompt | null>(null);
  const [preferredPromptId, setPreferredPromptId] = useState<number | null>(null);
  const playerRef = useRef<StreamVideoPlayerHandle>(null);
  /** streamUrl 变更会清空选区；项目回显需在其后写入 */
  const pendingRangesRef = useRef<TimeRange[] | null>(null);
  const streamUrlRef = useRef('');
  const savedSnapshotRef = useRef('');
  const baselineSyncedRef = useRef(false);
  const [promptSelectionReady, setPromptSelectionReady] = useState(false);

  useAppSEO({
    title: resolveSlicePageTitle({
      projectName,
      saved: Boolean(projectId),
    }),
    path: sourceVideoId
      ? buildSourceVideoSliceLink(sourceVideoId, { projectId: projectId || undefined })
      : '/source-videos',
    robots: 'noindex, nofollow',
  });

  const streamUrl = video?.live_url?.trim() ?? '';
  streamUrlRef.current = streamUrl;
  const hasVideoUrl = Boolean(streamUrl);
  const canPreview = hasVideoUrl && isPlayableVideoUrl(streamUrl);
  const videoFormatLabel = useMemo(() => getVideoFormatLabel(streamUrl), [streamUrl]);

  const resetDirtyBaseline = useCallback(
    (baseline: {
      ranges: TimeRange[];
      promptId: number | null;
      projectName: string;
    }) => {
      const snapshot = serializeTimelineSliceProjectState(baseline);
      savedSnapshotRef.current = snapshot;
      baselineSyncedRef.current = true;
    },
    []
  );

  const getIsDirty = useCallback(() => {
    if (loading || !video || !baselineSyncedRef.current) return false;

    return (
      serializeTimelineSliceProjectState({
        ranges: selectedRanges,
        promptId: selectedPrompt?.id ?? preferredPromptId,
        projectName,
      }) !== savedSnapshotRef.current
    );
  }, [loading, preferredPromptId, projectName, selectedPrompt?.id, selectedRanges, video]);

  const { confirmLeave } = useSliceProjectLeaveGuard(getIsDirty);

  useEffect(() => {
    baselineSyncedRef.current = false;
    savedSnapshotRef.current = '';
    setPromptSelectionReady(false);
  }, [projectId, sourceVideoId]);

  useEffect(() => {
    setVideoDuration(0);
    setCurrentTime(0);
    setActiveRangeId(null);
    setVideoError(null);
    baselineSyncedRef.current = false;

    if (pendingRangesRef.current) {
      setSelectedRanges(pendingRangesRef.current);
      pendingRangesRef.current = null;
    } else {
      setSelectedRanges([]);
    }
  }, [streamUrl]);

  useEffect(() => {
    if (loading || !video || baselineSyncedRef.current) return;

    // 回显提示词尚在分页加载
    if (preferredPromptId != null && !selectedPrompt) return;

    // 等待默认提示词自动选中；无可用提示词时由 promptSelectionReady 放行
    if (!selectedPrompt && !promptSelectionReady) return;

    resetDirtyBaseline({
      ranges: selectedRanges,
      promptId: selectedPrompt?.id ?? preferredPromptId,
      projectName,
    });
  }, [
    loading,
    preferredPromptId,
    projectName,
    promptSelectionReady,
    resetDirtyBaseline,
    selectedPrompt,
    selectedRanges,
    video,
  ]);

  // 基线建立时提示词尚未选中（promptId=0），默认提示词就绪后仅对齐 prompt，不吸收后续选区编辑
  useEffect(() => {
    if (loading || !video || !baselineSyncedRef.current || !selectedPrompt) return;

    let baselinePromptId = 0;
    try {
      baselinePromptId = JSON.parse(savedSnapshotRef.current).promptId ?? 0;
    } catch {
      return;
    }

    if (baselinePromptId !== 0 || selectedPrompt.id === 0) return;

    resetDirtyBaseline({
      ranges: selectedRanges,
      promptId: selectedPrompt.id,
      projectName,
    });
  }, [loading, projectName, resetDirtyBaseline, selectedPrompt, selectedRanges, video]);

  const loadPageData = useCallback(async () => {
    if (!sourceVideoId) return;

    setLoading(true);
    pendingRangesRef.current = null;
    setPreferredPromptId(null);
    setSelectedPrompt(null);
    setPromptSelectionReady(false);
    if (!projectId) {
      setProjectName('');
      setProjectTitle('');
      setProjectDescription('');
      setProjectTopics([]);
    }

    try {
      // 无 projectId：源视频入口只拉详情；有 projectId：再拉项目并回填 clips0 / prompt_id
      const [videoRes, projectSettled] = await Promise.all([
        fetchSourceVideoDetail(sourceVideoId),
        projectId ? fetchSliceProjectDetail(projectId).catch(() => null) : Promise.resolve(null),
      ]);

      if (videoRes.code !== 0) {
        toast.notify.error(videoRes.message || '加载源视频失败');
        setVideo(null);
        setParagraphs([]);
        return;
      }

      const nextStreamUrl = videoRes.data.live_url?.trim() ?? '';
      const sameStream = streamUrlRef.current === nextStreamUrl;

      setVideo(videoRes.data);
      setParagraphs(
        normalizeTranscriptParagraphs(asrParagraphsToTranscriptParagraphs(videoRes.data.asr_paragraphs))
      );

      const projectRes = projectId ? projectSettled : null;
      const clips0 =
        projectRes?.code === 0 && projectRes.data ? projectRes.data.clips0 : undefined;
      const ranges = resolveSelectedRanges(clips0);
      const loadedProjectName =
        projectRes?.code === 0 && projectRes.data ? projectRes.data.name?.trim() || '' : '';
      const promptId =
        projectRes?.code === 0 && projectRes.data
          ? Number(projectRes.data.prompt_id ?? 0)
          : 0;
      const loadedPromptId = promptId > 0 ? promptId : null;

      // streamUrl 不变时不会触发清理 effect，需直接回填
      if (sameStream) {
        setSelectedRanges(ranges);
        pendingRangesRef.current = null;
      } else {
        pendingRangesRef.current = ranges;
      }

      if (projectRes?.code === 0 && projectRes.data) {
        setProjectName(loadedProjectName);
        setProjectTitle(projectRes.data.title || '');
        setProjectDescription(projectRes.data.description || '');
        setProjectTopics(projectRes.data.topics ?? []);
        setPreferredPromptId(loadedPromptId);
      } else if (projectId) {
        toast.notify.warning(projectRes?.message || '剪辑项目加载失败');
      }
    } catch (error) {
      setVideo(null);
      setParagraphs([]);
      if (error instanceof AppError) {
        showAppError(error);
      } else {
        toast.notify.error('加载页面数据失败');
      }
    } finally {
      setLoading(false);
    }
  }, [projectId, sourceVideoId]);

  useEffect(() => {
    void loadPageData();
  }, [loadPageData]);

  const isTimelineReady = videoDuration > 0 && !videoError;
  const isTimelineLoading = canPreview && !videoError && videoDuration === 0;

  const aiPreviewRanges = useMemo(
    () => asrSummariesToTimeRanges(video?.asr_summaries),
    [video?.asr_summaries]
  );

  const handleDurationChange = useCallback((duration: number) => {
    setVideoDuration(duration);
  }, []);

  const handlePlaybackError = useCallback((message: string) => {
    setVideoError(message);
  }, []);

  useEffect(() => {
    const video = playerRef.current?.video;
    if (!video || !isTimelineReady) return;

    const syncCurrentTime = () => {
      if (!Number.isFinite(video.duration)) return;
      setCurrentTime(video.currentTime || 0);
    };

    video.addEventListener('timeupdate', syncCurrentTime);
    video.addEventListener('seeked', syncCurrentTime);

    return () => {
      video.removeEventListener('timeupdate', syncCurrentTime);
      video.removeEventListener('seeked', syncCurrentTime);
    };
  }, [isTimelineReady, streamUrl]);

  // 选中片段播放到结尾后自动取消选中并暂停
  useEffect(() => {
    if (!activeRangeId) return;

    const activeRange = selectedRanges.find((range) => range.id === activeRangeId);
    if (!activeRange) return;

    const video = playerRef.current?.video;
    if (!video || video.paused) return;

    if (currentTime >= activeRange.end - 0.05) {
      video.pause();
      video.currentTime = Math.min(activeRange.end, video.duration || activeRange.end);
      setCurrentTime(video.currentTime);
      setActiveRangeId(null);
    }
  }, [activeRangeId, currentTime, selectedRanges]);

  const handleTimeChange = useCallback((time: number) => {
    const video = playerRef.current?.video;
    if (video) {
      video.currentTime = time;
      if (video.paused) {
        void video.play().catch(() => undefined);
      }
    }
    setCurrentTime(time);
  }, []);

  const handleRangeSelect = useCallback(
    (range: TimeRange) => {
      setSelectedRanges((prev) => [...prev, range]);
      if (autoPlayOnSelect) {
        handleTimeChange(range.start);
      }
    },
    [autoPlayOnSelect, handleTimeChange]
  );

  const handlePreviewRangeClick = useCallback(
    (preview: TimeRange) => {
      if (projectTaskReadOnly) {
        handleTimeChange(preview.start);
        return;
      }

      const alreadySelected = selectedRanges.some(
        (item) =>
          Math.abs(item.start - preview.start) < 0.05 && Math.abs(item.end - preview.end) < 0.05
      );
      if (alreadySelected) {
        toast.notify.info('该 AI 选片已在已选片段中');
        handleTimeChange(preview.start);
        return;
      }

      const nextRange: TimeRange = {
        id: `range-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`,
        start: preview.start,
        end: preview.end,
      };
      setSelectedRanges((prev) => [...prev, nextRange]);
      setActiveRangeId(nextRange.id);
      handleTimeChange(preview.start);
    },
    [handleTimeChange, projectTaskReadOnly, selectedRanges]
  );

  const handleRangeDelete = useCallback((rangeId: string) => {
    setActiveRangeId((current) => (current === rangeId ? null : current));
    setSelectedRanges((prev) => prev.filter((item) => item.id !== rangeId));
  }, []);

  const handleActiveRangeSelect = useCallback(
    (rangeId: string, start: number) => {
      setActiveRangeId(rangeId);
      handleTimeChange(start);
    },
    [handleTimeChange]
  );

  const handleClearAllRanges = useCallback(() => {
    setSelectedRanges([]);
    setActiveRangeId(null);
  }, []);

  const handleRangeUpdate = useCallback((updated: TimeRange) => {
    setSelectedRanges((prev) =>
      prev.map((item) => (item.id === updated.id ? { ...item, ...updated } : item))
    );
  }, []);

  const totalSelectedDuration = useMemo(
    () => selectedRanges.reduce((sum, range) => sum + (range.end - range.start), 0),
    [selectedRanges]
  );

  const handleSubmit = useCallback(async () => {
    if (projectTaskReadOnly) {
      toast.notify.warning('项目有进行中任务，当前仅可查看');
      return;
    }
    if (!video) return;

    if (selectedRanges.length === 0) {
      toast.notify.warning('请先选择至少一个时间段');
      return;
    }

    if (totalSelectedDuration < MIN_TOTAL_DURATION) {
      toast.notify.warning(`已选时长需不少于 ${MIN_TOTAL_DURATION / 60} 分钟`);
      return;
    }

    if (!selectedPrompt) {
      toast.notify.warning('请先选择一个 AI 提示词');
      return;
    }

    setSubmitting(true);
    try {
      const response = await submitClip({
        live_id: video.id,
        prompt_id: selectedPrompt.id,
        clips0: selectedRangesToClips0(selectedRanges),
        project_source: 'timeline',
      });

      if (response.code !== 0) {
        toast.notify.error(response.message || '提交失败');
        return;
      }

      const total = response.data?.total ?? response.data?.list?.length ?? 1;
      toast.notify.success(
        '创建成功',
        total > 1 ? `已创建 ${total} 个任务，可前往任务管理查看` : '可前往任务管理查看'
      );
      resetDirtyBaseline({
        ranges: selectedRanges,
        promptId: selectedPrompt.id,
        projectName,
      });
    } catch (error) {
      if (error instanceof AppError) {
        showAppError(error);
      } else {
        toast.notify.error('提交失败');
      }
    } finally {
      setSubmitting(false);
    }
  }, [projectName, projectTaskReadOnly, resetDirtyBaseline, selectedPrompt, selectedRanges, totalSelectedDuration, video]);

  const handleAiSelect = useCallback(async () => {
    if (projectTaskReadOnly) {
      toast.notify.warning('项目有进行中任务，当前仅可查看');
      return;
    }
    if (!video) return;

    if (selectedRanges.length === 0) {
      toast.notify.warning('请先选择至少一个时间段');
      return;
    }

    if (totalSelectedDuration < MIN_TOTAL_DURATION) {
      toast.notify.warning(`已选时长需不少于 ${MIN_TOTAL_DURATION / 60} 分钟`);
      return;
    }

    if (!selectedPrompt) {
      toast.notify.warning('请先选择一个 AI 提示词');
      return;
    }

    setAiSelecting(true);
    try {
      const response = await submitAiSliceSelection({
        live_id: video.id,
        prompt_id: selectedPrompt.id,
        clips0: selectedRangesToClips0(selectedRanges),
        project_source: 'manual',
      });

      if (response.code !== 0) {
        toast.notify.error(response.message || 'AI 选片任务提交失败');
        return;
      }

      const total = response.data?.total ?? response.data?.list?.length ?? 1;
      toast.notify.success(
        '创建成功',
        total > 1 ? `已创建 ${total} 个任务，可前往任务管理查看` : '可前往任务管理查看'
      );
      resetDirtyBaseline({
        ranges: selectedRanges,
        promptId: selectedPrompt.id,
        projectName,
      });
    } catch (error) {
      if (error instanceof AppError) {
        showAppError(error);
      } else {
        toast.notify.error('AI 选片失败');
      }
    } finally {
      setAiSelecting(false);
    }
  }, [projectName, projectTaskReadOnly, resetDirtyBaseline, selectedPrompt, selectedRanges, totalSelectedDuration, video]);

  const handleSwitchToManual = useCallback(() => {
    confirmLeave(() => {
      if (projectId) {
        void updateSliceProject(projectId, { project_source: 'manual' });
      }

      navigate(buildManualVideoSliceLink(sourceVideoId, { projectId: projectId || undefined }), {
        state: { from: entryFrom },
      });
    });
  }, [confirmLeave, entryFrom, navigate, projectId, sourceVideoId]);

  const handleLeaveNavigate = useCallback(
    (to: string) => {
      confirmLeave(() => navigate(to));
    },
    [confirmLeave, navigate]
  );

  const pageTitle = useMemo(
    () =>
      resolveSlicePageTitle({
        projectName,
        saved: Boolean(projectId),
      }),
    [projectId, projectName]
  );

  const breadcrumbItems = useMemo(
    () =>
      buildSliceBreadcrumbItems({
        entryFrom,
        sourceVideoId,
        pageKind: 'timeline',
        videoName: video?.name,
        projectName,
        saved: Boolean(projectId),
        onNavigate: handleLeaveNavigate,
      }),
    [entryFrom, handleLeaveNavigate, projectId, projectName, sourceVideoId, video?.name]
  );

  if (loading) {
    return <SourceVideoSlicePageSkeleton breadcrumbItems={breadcrumbItems} />;
  }

  if (!video) {
    return (
      <div className="slice-page slice-page_timeline">
        <SlicePageHeader breadcrumbItems={breadcrumbItems} title="" />
        <div className="slice-page-empty-shell">
          <SlicePageEmptyState variant="video-unavailable" entryFrom={entryFrom} />
        </div>
      </div>
    );
  }

  const pageHeader = (
    <SlicePageHeader
      breadcrumbItems={breadcrumbItems}
      title={pageTitle}
      extra={
        projectTitle.trim() || projectDescription.trim() || projectTopics.length > 0 ? (
          <SliceProjectMetaBar
            title={projectTitle}
            description={projectDescription}
            topics={projectTopics}
          />
        ) : undefined
      }
      actions={
        <>
          <Button onClick={() => setSourceModalVisible(true)}>查看播放源</Button>
          <Button className="slice-mode-switch-btn" onClick={handleSwitchToManual}>
            切换到人工切片
          </Button>
        </>
      }
    />
  );

  return (
    <div className="slice-page slice-page_timeline">
      {pageHeader}
      <SliceProjectTaskReadOnlyAlert
        visible={projectId != null}
        projectName={projectName}
        runningTaskCount={runningTaskCount}
      />

      {!hasVideoUrl ? (
        <div className="slice-page-empty-shell">
          <SlicePageEmptyState variant="no-playback-url" entryFrom={entryFrom} />
        </div>
      ) : !canPreview ? (
        <div className="slice-page-empty-shell">
          <SlicePageEmptyState variant="unsupported-format" entryFrom={entryFrom} />
        </div>
      ) : (
        <div className="slice-workspace-card">
          <div className="slice-main-section">
            <div className="slice-video-section">
              <SliceVideoPlayer
                ref={playerRef}
                url={streamUrl}
                className="slice-video"
                errorClassName="slice-video-error"
                paragraphs={paragraphs}
                currentTime={currentTime}
                onSeek={handleTimeChange}
                screenshotBaseName={video?.name ?? 'video-screenshot'}
                onDurationChange={handleDurationChange}
                onPlaybackError={handlePlaybackError}
              />
            </div>

            <PromptPickerPanel
              selectedId={selectedPrompt?.id ?? null}
              preferredId={preferredPromptId}
              onSelect={setSelectedPrompt}
              onInitialSelectionReady={() => setPromptSelectionReady(true)}
            />
          </div>

          {isTimelineLoading && <TimelineLoadingSkeleton />}

          {isTimelineReady && (
            <div className="slice-timeline-section">
              <SelectedSegmentsPanel
                videoDuration={videoDuration}
                selectedRanges={selectedRanges}
                totalSelectedDuration={totalSelectedDuration}
                minTotalDuration={MIN_TOTAL_DURATION}
                submitting={submitting}
                aiSelecting={aiSelecting}
                zoomLevel={timelineZoomLevel}
                onZoomLevelChange={setTimelineZoomLevel}
                activeRangeId={activeRangeId}
                onActiveRangeSelect={handleActiveRangeSelect}
                onSubmit={() => void handleSubmit()}
                onAiSelect={() => void handleAiSelect()}
                onClearAll={handleClearAllRanges}
                onRangeDelete={handleRangeDelete}
                hasSelectedPrompt={selectedPrompt != null}
                readOnly={projectTaskReadOnly}
              />
              <VideoTimeline
                duration={videoDuration}
                currentTime={currentTime}
                selectedRanges={selectedRanges}
                previewRanges={aiPreviewRanges}
                zoomLevel={timelineZoomLevel}
                onZoomLevelChange={setTimelineZoomLevel}
                activeRangeId={activeRangeId}
                onActiveRangeChange={setActiveRangeId}
                onTimeChange={handleTimeChange}
                onRangeSelect={handleRangeSelect}
                onRangeDelete={handleRangeDelete}
                onRangeUpdate={handleRangeUpdate}
                onPreviewRangeClick={handlePreviewRangeClick}
                readOnly={projectTaskReadOnly}
              />
            </div>
          )}
        </div>
      )}

      <Drawer
        open={sourceModalVisible}
        placement="right"
        width="min(520px, 100vw)"
        title={null}
        closable={false}
        destroyOnClose
        className="slice-source-drawer"
        onClose={() => setSourceModalVisible(false)}
      >
        <div className="slice-source-drawer__layout">
          <header className="slice-source-drawer__header">
            <div className="slice-source-drawer__header-main">
              <h3 className="slice-source-drawer__title">播放源信息</h3>
              <p className="slice-source-drawer__meta">{video.name}</p>
            </div>
            <button
              type="button"
              className="slice-source-drawer__close"
              aria-label="关闭"
              onClick={() => setSourceModalVisible(false)}
            >
              <LuX size={18} />
            </button>
          </header>

          <div className="slice-source-drawer__body">
            <Descriptions column={1} size="small" className="slice-source-descriptions">
              <Descriptions.Item label="源视频名称">{video.name}</Descriptions.Item>
              <Descriptions.Item label="备注">{video.remark || '-'}</Descriptions.Item>
              <Descriptions.Item label="直播地址">
                <CopyableText
                  text={video.live_url}
                  layout="paragraph"
                  className="slice-source-url"
                  emptyFallback="-"
                />
              </Descriptions.Item>
              <Descriptions.Item label="时长">
                {video.duration > 0 ? formatVideoDurationMs(video.duration) : '-'}
              </Descriptions.Item>
              <Descriptions.Item label="创建时间">{formatToDateTime(video.created_at)}</Descriptions.Item>
              <Descriptions.Item label="预览状态">
                {canPreview
                  ? `支持浏览器预览（${videoFormatLabel}）`
                  : hasVideoUrl
                    ? '格式不受支持'
                    : '暂无播放地址'}
              </Descriptions.Item>
            </Descriptions>
          </div>
        </div>
      </Drawer>
    </div>
  );
};

export default SourceVideoSlicePage;
