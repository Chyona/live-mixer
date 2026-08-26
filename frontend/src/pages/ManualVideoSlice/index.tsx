import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useLocation, useNavigate, useParams, useSearchParams } from 'react-router-dom';
import { Button, Space, Tooltip } from 'antd';
import { LuDownload } from 'react-icons/lu';
import SliceVideoPlayer from '~/components/SliceVideoPlayer';
import type { StreamVideoPlayerHandle } from '~/components/StreamVideoPlayer';
import SlicePageHeader, { SliceProjectMetaBar } from '~/components/SlicePageHeader';
import SlicePageEmptyState from '~/components/SlicePageEmptyState';
import ManualVideoSlicePageSkeleton from './ManualVideoSlicePageSkeleton';
import { useAppSEO } from '~/hooks/useAppSEO';
import { useSliceProjectTaskStats } from '~/hooks/useSliceProjectTaskStats';
import {
  SliceProjectTaskReadOnlyAlert,
} from '~/components/SliceProjectTaskStatus';
import { AppError } from '~/services/http';
import {
  downloadSourceVideoAsrSubtitle,
  fetchSourceVideoDetail,
  type SourceVideo,
} from '~/services/sourceVideo';
import {
  fetchSliceProjectDetail,
  saveSliceProject,
  toSliceProjectClips,
  updateSliceProject,
} from '~/services/sliceProject';
import { submitDraft } from '~/services/slice';
import { formatToDateTime } from '~/utils/date';
import { showAppError, toast } from '~/utils/toast';
import { isPlayableVideoUrl } from '~/utils/videoUrl';
import { useSliceEntryFrom } from '~/hooks/useSliceEntryFrom';
import { useSliceProjectLeaveGuard } from '~/context/SliceLeaveGuardContext';
import type { SliceEditorEntryFrom } from '~/routes/links';
import {
  buildManualVideoSliceLink,
  buildSourceVideoSliceLink,
  buildTasksListLink,
  parseProjectId,
} from '~/routes/links';
import { mergeDebugAsrKeySearchParams } from '~/utils/asrParagraphsKey';
import { buildSliceBreadcrumbItems, resolveSlicePageTitle } from '~/utils/sliceBreadcrumbs';
import { serializeManualSliceProjectState } from '~/utils/sliceProjectDirty';
import TranscriptPanel from './components/TranscriptPanel';
import VideoTranscriptResizeHandle from './components/VideoTranscriptResizeHandle';
import SelectedCopyPanel from './components/SelectedCopyPanel';
import SegmentPreviewModal from './components/SegmentPreviewModal';
import SaveDraftModal from './components/SaveDraftModal';
import type { AiSegment, SelectedCopySegment, TranscriptParagraph } from './types';
import {
  deleteSelectedRangeFromSegment,
  resolveCopySegmentWords,
  adjustSegmentEdge,
  findActiveSegment,
  buildTranscriptHighlight,
  buildSelectedCopyHighlightRanges,
  getParagraphText,
  getTextSelectionOffsets,
  asrParagraphsToTranscriptParagraphs,
  asrSummariesToAiSegments,
  buildCopySegmentsFromAiSegment,
  normalizeTranscriptParagraphs,
  sanitizeDownloadFilename,
  sanitizeSelectedCopySegments,
  clampCopySegmentPlaybackBounds,
  insertSegmentsByTimelineProximity,
} from './utils';

interface ManualSliceLocationState {
  from?: SliceEditorEntryFrom;
  aiSelectedSegments?: SelectedCopySegment[];
}

const MAX_TOTAL_DURATION = 2 * 60 * 60;
const DRAFT_STORAGE_KEY = 'manual-slice-draft-name';

/** 人工切片项目自动命名：人工切片_时间 */
function buildManualProjectAutoName() {
  return `人工切片_${formatToDateTime(Date.now(), 'YYYY-MM-DD_HH:mm:ss')}`;
}

const MANUAL_SLICE_SAVE_NOTIFY_DESC =
  '您可以继续调整内容，或直接提交成片。提交后将创建任务并跳转至任务列表。';

const ManualVideoSlicePage = () => {
  const { id: sourceVideoId = '' } = useParams();
  const [searchParams] = useSearchParams();
  const navigate = useNavigate();
  const location = useLocation();
  const entryFrom = useSliceEntryFrom();
  const playerRef = useRef<StreamVideoPlayerHandle>(null);
  const panelLeftRef = useRef<HTMLDivElement>(null);
  const videoBlockRef = useRef<HTMLDivElement>(null);
  const lastCurrentTimeRef = useRef(0);

  /** 项目管理进入时带 ?projectId=；源视频首次保存后也会回写 */
  const projectIdFromQuery = parseProjectId(searchParams.get('projectId'));
  const [projectId, setProjectId] = useState<number | null>(projectIdFromQuery);
  const {
    runningTaskCount,
    readOnly: projectTaskReadOnly,
  } = useSliceProjectTaskStats(projectId);
  /** 保存/另存为后回写 URL，不触发整页数据重载 */
  const skipProjectReloadRef = useRef(false);

  const [loading, setLoading] = useState(true);
  const [video, setVideo] = useState<SourceVideo | null>(null);
  const [paragraphs, setParagraphs] = useState<TranscriptParagraph[]>([]);
  const [videoDuration, setVideoDuration] = useState(0);
  const [currentTime, setCurrentTime] = useState(0);
  const [isVideoPlaying, setIsVideoPlaying] = useState(false);
  const [keyword, setKeyword] = useState('');
  const [activeMatchIndex, setActiveMatchIndex] = useState(0);
  const [videoPanelHeight, setVideoPanelHeight] = useState<number | null>(null);
  const [selectedSegments, setSelectedSegments] = useState<SelectedCopySegment[]>([]);
  const [activeSegmentId, setActiveSegmentId] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);
  const [previewOpen, setPreviewOpen] = useState(false);
  const [saveModalOpen, setSaveModalOpen] = useState(false);
  const [saveModalMode, setSaveModalMode] = useState<'create' | 'saveAs' | 'export'>('saveAs');
  const [savingProject, setSavingProject] = useState(false);
  const [downloadingSubtitle, setDownloadingSubtitle] = useState(false);
  const [enableCaptions, setEnableCaptions] = useState(false);
  const [draftName, setDraftName] = useState(() => localStorage.getItem(DRAFT_STORAGE_KEY) ?? '');
  const [projectRemark, setProjectRemark] = useState('');
  const [projectTitle, setProjectTitle] = useState('');
  const [projectDescription, setProjectDescription] = useState('');
  const [projectTopics, setProjectTopics] = useState<string[]>([]);
  const savedSnapshotRef = useRef('');
  const baselineSyncedRef = useRef(false);

  useEffect(() => {
    setProjectId(projectIdFromQuery);
  }, [projectIdFromQuery]);

  useAppSEO({
    title: resolveSlicePageTitle({
      projectName: draftName,
      saved: Boolean(projectId),
    }),
    path: sourceVideoId
      ? buildManualVideoSliceLink(sourceVideoId, { projectId: projectId || undefined })
      : '/source-videos',
    robots: 'noindex, nofollow',
  });

  const streamUrl = video?.live_url?.trim() ?? '';
  const canPreview = Boolean(streamUrl) && isPlayableVideoUrl(streamUrl);

  const getIsDirty = useCallback(() => {
    if (loading || !video || !baselineSyncedRef.current) return false;
    return (
      serializeManualSliceProjectState({
        segments: selectedSegments,
        enableCaptions,
        draftName,
        projectRemark,
      }) !== savedSnapshotRef.current
    );
  }, [
    draftName,
    enableCaptions,
    loading,
    projectRemark,
    selectedSegments,
    video,
  ]);

  const { confirmLeave } = useSliceProjectLeaveGuard(getIsDirty);

  const resetDirtyBaseline = useCallback(
    (baseline: {
      segments: SelectedCopySegment[];
      enableCaptions: boolean;
      draftName: string;
      projectRemark: string;
    }) => {
      savedSnapshotRef.current = serializeManualSliceProjectState(baseline);
      baselineSyncedRef.current = true;
    },
    []
  );

  useEffect(() => {
    baselineSyncedRef.current = false;
    savedSnapshotRef.current = '';
  }, [sourceVideoId, projectIdFromQuery]);

  const speakers = useMemo(
    () => [...new Set(paragraphs.map((item) => item.speaker))],
    [paragraphs]
  );

  const matchParagraphIds = useMemo(() => {
    const term = keyword.trim();
    if (!term) return [];
    const lower = term.toLowerCase();
    return paragraphs
      .filter((paragraph) => getParagraphText(paragraph).toLowerCase().includes(lower))
      .map((paragraph) => paragraph.id);
  }, [keyword, paragraphs]);

  const activeSync = useMemo(
    () => findActiveSegment(paragraphs, currentTime),
    [paragraphs, currentTime]
  );

  const activeParagraphId = activeSync?.paragraphId ?? null;
  const activeTranscriptSegmentId = activeSync?.segmentId ?? null;

  const transcriptHighlight = useMemo(
    () =>
      buildTranscriptHighlight({
        playbackSync:
          activeParagraphId && activeTranscriptSegmentId
            ? { paragraphId: activeParagraphId, segmentId: activeTranscriptSegmentId }
            : null,
      }),
    [activeParagraphId, activeTranscriptSegmentId]
  );

  const selectedCopyHighlights = useMemo(
    () => buildSelectedCopyHighlightRanges(selectedSegments, paragraphs),
    [selectedSegments, paragraphs]
  );

  const handleDurationChange = useCallback((duration: number) => {
    setVideoDuration((prev) => (Math.abs(prev - duration) < 0.001 ? prev : duration));
  }, []);

  const syncProjectIdInUrl = useCallback(
    (nextProjectId: number, options?: { reload?: boolean }) => {
      if (!nextProjectId) return;

      setProjectId(nextProjectId);
      if (nextProjectId === projectIdFromQuery) return;

      if (options?.reload === false) {
        skipProjectReloadRef.current = true;
      }

      const nextSearch = mergeDebugAsrKeySearchParams(new URLSearchParams(searchParams));
      nextSearch.set('projectId', String(nextProjectId));
      navigate(
        { pathname: location.pathname, search: `?${nextSearch.toString()}` },
        { replace: true, state: location.state }
      );
    },
    [location.pathname, location.state, navigate, projectIdFromQuery, searchParams]
  );

  const loadPageData = useCallback(async () => {
    if (!sourceVideoId) return;

    const locationState = location.state as ManualSliceLocationState | null;
    const hasAiSegments = Boolean(locationState?.aiSelectedSegments?.length);

    setLoading(true);
    try {
      // 无 projectId：源视频入口，只拉源视频详情（干净页）
      // 有 projectId：项目管理入口，再拉项目详情并回填片段
      const [videoRes, projectSettled] = await Promise.all([
        fetchSourceVideoDetail(sourceVideoId),
        projectIdFromQuery
          ? fetchSliceProjectDetail(projectIdFromQuery).catch(() => null)
          : Promise.resolve(null),
      ]);

      if (videoRes.code !== 0) {
        toast.notify.error(videoRes.message || '加载源视频失败');
        setVideo(null);
        setParagraphs([]);
        return;
      }

      setVideo(videoRes.data);
      const normalizedParagraphs = normalizeTranscriptParagraphs(
        asrParagraphsToTranscriptParagraphs(videoRes.data.asr_paragraphs)
      );
      setParagraphs(normalizedParagraphs);
      const nextDraftName = buildManualProjectAutoName();
      const resolvedDraftName = localStorage.getItem(DRAFT_STORAGE_KEY)?.trim() || nextDraftName;
      setDraftName(resolvedDraftName);

      if (!projectIdFromQuery) {
        setProjectId(null);
        setProjectRemark('');
        setProjectTitle('');
        setProjectDescription('');
        setProjectTopics([]);
        const nextSegments = hasAiSegments ? (locationState?.aiSelectedSegments ?? []) : [];
        if (!hasAiSegments) {
          setSelectedSegments([]);
        }
        return;
      }

      const projectRes = projectSettled;
      if (projectRes?.code === 0 && projectRes.data) {
        const loadedProjectId = projectRes.data.id || projectIdFromQuery;
        setProjectId(loadedProjectId);
        const loadedRemark = projectRes.data.remark || '';
        const loadedEnableCaptions = Boolean(projectRes.data.enable_captions);
        const loadedDraftName = projectRes.data.name;
        setProjectRemark(loadedRemark);
        setProjectTitle(projectRes.data.title || '');
        setProjectDescription(projectRes.data.description || '');
        setProjectTopics(projectRes.data.topics ?? []);
        setEnableCaptions(loadedEnableCaptions);
        let loadedSegments: SelectedCopySegment[] = [];
        if (!hasAiSegments && projectRes.data.segments.length > 0) {
          const videoDurationSec = Number(videoRes.data.duration) || 0;
          loadedSegments = sanitizeSelectedCopySegments(
            projectRes.data.segments,
            normalizedParagraphs,
            videoDurationSec
          );
          setSelectedSegments(loadedSegments);
          setDraftName(loadedDraftName);
        }
      } else {
        setProjectTitle('');
        setProjectDescription('');
        setProjectTopics([]);
        toast.notify.warning(projectRes?.message || '剪辑项目加载失败');
      }
    } catch (error) {
      setVideo(null);
      if (error instanceof AppError) {
        showAppError(error);
      } else {
        toast.notify.error('加载页面数据失败');
      }
    } finally {
      setLoading(false);
    }
  }, [location.state, projectIdFromQuery, sourceVideoId]);

  useEffect(() => {
    if (skipProjectReloadRef.current) {
      skipProjectReloadRef.current = false;
      return;
    }
    void loadPageData();
  }, [loadPageData]);

  useEffect(() => {
    const state = location.state as ManualSliceLocationState | null;
    const aiSelectedSegments = state?.aiSelectedSegments;

    if (!aiSelectedSegments?.length) return;

    baselineSyncedRef.current = false;
    setSelectedSegments(aiSelectedSegments);
    toast.notify.success('AI 选片结果已载入，可继续编辑文案片段');
    const nextSearch = mergeDebugAsrKeySearchParams(new URLSearchParams(location.search));
    const search = nextSearch.toString();
    navigate(
      { pathname: location.pathname, search: search ? `?${search}` : '' },
      { replace: true, state: null }
    );
  }, [location.pathname, location.search, location.state, navigate]);

  useEffect(() => {
    lastCurrentTimeRef.current = 0;
    setVideoDuration(0);
    setCurrentTime(0);
    setIsVideoPlaying(false);
    setActiveSegmentId(null);
  }, [streamUrl]);

  useEffect(() => {
    // 切换源视频时清空文案预览；同视频加载播放地址时不要清，避免盖掉项目回填
    setSelectedSegments([]);
    setActiveSegmentId(null);
  }, [sourceVideoId]);

  useEffect(() => {
    setActiveMatchIndex(0);
  }, [keyword, matchParagraphIds.length]);

  useEffect(() => {
    if (!paragraphs.length || !selectedSegments.length) return;

    const next = sanitizeSelectedCopySegments(selectedSegments, paragraphs, videoDuration);
    const unchanged = next.every((segment, index) => {
      const prev = selectedSegments[index];
      if (!prev) return false;
      return (
        segment.start === prev.start &&
        segment.end === prev.end &&
        segment.sourceParagraphId === prev.sourceParagraphId &&
        segment.speaker === prev.speaker
      );
    });
    if (unchanged) return;

    baselineSyncedRef.current = false;
    setSelectedSegments(next);
  }, [paragraphs, selectedSegments, videoDuration]);

  useEffect(() => {
    if (loading || !video || baselineSyncedRef.current) return;
    if (selectedSegments.length > 0 && videoDuration <= 0) return;

    resetDirtyBaseline({
      segments: selectedSegments,
      enableCaptions,
      draftName,
      projectRemark,
    });
  }, [
    draftName,
    enableCaptions,
    loading,
    projectRemark,
    resetDirtyBaseline,
    selectedSegments,
    video,
    videoDuration,
  ]);

  useEffect(() => {
    const videoEl = playerRef.current?.video;
    if (!videoEl || videoDuration <= 0) return;

    const syncCurrentTime = () => {
      const nextTime = videoEl.currentTime || 0;
      if (Math.abs(nextTime - lastCurrentTimeRef.current) < 0.04) return;
      lastCurrentTimeRef.current = nextTime;
      setCurrentTime(nextTime);
    };

    videoEl.addEventListener('timeupdate', syncCurrentTime);
    videoEl.addEventListener('seeked', syncCurrentTime);

    return () => {
      videoEl.removeEventListener('timeupdate', syncCurrentTime);
      videoEl.removeEventListener('seeked', syncCurrentTime);
    };
  }, [videoDuration, streamUrl]);

  useEffect(() => {
    const videoEl = playerRef.current?.video;
    if (!videoEl || videoDuration <= 0) {
      setIsVideoPlaying((prev) => (prev ? false : prev));
      return;
    }

    const syncPlayingState = () => {
      const nextPlaying = !videoEl.paused && !videoEl.ended;
      setIsVideoPlaying((prev) => (prev === nextPlaying ? prev : nextPlaying));
    };

    syncPlayingState();
    videoEl.addEventListener('play', syncPlayingState);
    videoEl.addEventListener('pause', syncPlayingState);
    videoEl.addEventListener('ended', syncPlayingState);

    return () => {
      videoEl.removeEventListener('play', syncPlayingState);
      videoEl.removeEventListener('pause', syncPlayingState);
      videoEl.removeEventListener('ended', syncPlayingState);
    };
  }, [videoDuration, streamUrl]);

  const handleSeek = useCallback((time: number) => {
    const videoEl = playerRef.current?.video;
    if (videoEl) {
      videoEl.currentTime = time;
      if (videoEl.paused) {
        void videoEl.play().catch(() => undefined);
      }
    }
    lastCurrentTimeRef.current = time;
    setCurrentTime(time);
  }, []);

  const handleSelectSegment = useCallback((segment: SelectedCopySegment | null) => {
    if (!segment) return;

    const sanitized = clampCopySegmentPlaybackBounds(segment, paragraphs, videoDuration);
    setSelectedSegments((prev) => insertSegmentsByTimelineProximity(prev, sanitized));
    setActiveSegmentId(sanitized.id);
    // 选片只加入预览，不打断当前播放进度
    toast.notify.success('已添加到文案预览');
  }, [paragraphs, videoDuration]);

  const aiSegments = useMemo(
    () => asrSummariesToAiSegments(video?.asr_summaries),
    [video?.asr_summaries]
  );

  const handleAddAiSegment = useCallback(
    (aiSegment: AiSegment) => {
      const segments = buildCopySegmentsFromAiSegment(paragraphs, aiSegment);
      if (!segments.length) {
        toast.notify.warning('该 AI 分段暂无可用文案，请先从左侧文案分段中选择');
        return;
      }

      const sanitized = segments.map((segment) =>
        clampCopySegmentPlaybackBounds(segment, paragraphs, videoDuration)
      );
      setSelectedSegments((prev) => insertSegmentsByTimelineProximity(prev, sanitized));
      setActiveSegmentId(sanitized[sanitized.length - 1]?.id ?? null);
      toast.notify.success(
        sanitized.length > 1
          ? `已添加 ${sanitized.length} 个片段到文案预览`
          : '已添加到文案预览'
      );
    },
    [paragraphs, videoDuration]
  );

  const handleDeleteSegment = useCallback((segmentId: string) => {
    setSelectedSegments((prev) => prev.filter((item) => item.id !== segmentId));
    setActiveSegmentId((current) => (current === segmentId ? null : current));
  }, []);

  const handleDeleteSelectedRange = useCallback((
    segmentId: string,
    textElement: HTMLElement | null,
    savedSelection?: { start: number; end: number } | null
  ) => {
    const liveOffsets = textElement ? getTextSelectionOffsets(textElement) : null;
    const savedOffsets =
      savedSelection && savedSelection.end > savedSelection.start
        ? { start: savedSelection.start, end: savedSelection.end }
        : null;
    const offsets = savedOffsets ?? liveOffsets;

    if (!offsets) {
      toast.notify.warning('请先在片段文案中选中要删除的内容');
      return;
    }

    const target = selectedSegments.find((item) => item.id === segmentId);
    if (!target) return;
    const words = resolveCopySegmentWords(target, paragraphs);
    const result = deleteSelectedRangeFromSegment(
      target,
      offsets.start,
      offsets.end,
      words,
      paragraphs
    );

    if (result === 'delete-all') {
      setSelectedSegments((prev) => prev.filter((item) => item.id !== segmentId));
      setActiveSegmentId((current) => (current === segmentId ? null : current));
      window.getSelection()?.removeAllRanges();
      toast.notify.success('已删除选中区间');
      return;
    }

    if (!result?.length) {
      toast.notify.warning('选中区间无法删除，请调整选区后重试');
      return;
    }

    setSelectedSegments((prev) => {
      const index = prev.findIndex((item) => item.id === segmentId);
      if (index < 0) return prev;

      const next = [...prev];
      next.splice(index, 1, ...result);
      return next;
    });

    window.getSelection()?.removeAllRanges();
    toast.notify.success('已删除选中区间');
  }, [paragraphs, selectedSegments]);

  const handleCopySegment = useCallback((segmentId: string) => {
    setSelectedSegments((prev) => {
      const target = prev.find((item) => item.id === segmentId);
      if (!target) return prev;

      const copy: SelectedCopySegment = {
        ...target,
        id: `copy-${Date.now()}-${Math.random().toString(36).slice(2, 7)}`,
        originStart: target.start,
        originEnd: target.end,
      };
      return [...prev, copy];
    });
    toast.notify.success('已复制片段');
  }, []);

  const handleAdjustSegment = useCallback(
    (segmentId: string, edge: 'start' | 'end', deltaSec: number) => {
      const index = selectedSegments.findIndex((item) => item.id === segmentId);
      if (index < 0) return;

      const result = adjustSegmentEdge(
        selectedSegments,
        index,
        edge,
        deltaSec,
        videoDuration,
        paragraphs
      );
      if (!result) {
        const expanding = deltaSec > 0;
        toast.notify.warning(
          expanding
            ? edge === 'start'
              ? '前方没有可扩展的留白'
              : '后方没有可扩展的留白'
            : edge === 'start'
              ? '前方没有可收回的留白'
              : '后方没有可收回的留白'
        );
        return;
      }
      setSelectedSegments(result.segments);
    },
    [paragraphs, selectedSegments, videoDuration]
  );

  const submitProjectDraft = useCallback(async () => {
      if (projectTaskReadOnly) {
        toast.notify.warning('项目有进行中任务，当前仅可查看');
        return;
      }
      if (!video || selectedSegments.length === 0) return;

      if (!projectId) {
        toast.notify.warning('请先保存剪辑项目后再提交');
        return;
      }

      setSubmitting(true);
      try {
        const response = await submitDraft({
          video_project_id: projectId,
          enable_captions: enableCaptions,
        });

        if (response.code !== 0) {
          toast.notify.error(response.message || '提交失败');
          return;
        }

        toast.notify.success('任务已提交，正在跳转到任务管理');
        navigate(buildTasksListLink());
      } catch (error) {
        if (error instanceof AppError) {
          showAppError(error);
        } else {
          toast.notify.error('提交失败');
        }
      } finally {
        setSubmitting(false);
      }
    },
    [enableCaptions, navigate, projectId, projectTaskReadOnly, selectedSegments.length, video]
  );

  const handleSubmit = useCallback(() => {
    void submitProjectDraft();
  }, [submitProjectDraft]);

  const handleSaveProject = useCallback(
    async (options?: { name?: string; remark?: string }) => {
      if (!sourceVideoId || !video) return;

      if (selectedSegments.length === 0) {
        toast.notify.warning('请先选择至少一个片段');
        return;
      }

      const nextName = options?.name?.trim() || draftName || buildManualProjectAutoName();
      const nextRemark = options?.remark ?? projectRemark;
      const payload = {
        live_id: video.id,
        name: nextName,
        remark: nextRemark,
        project_source: 'manual' as const,
        enable_captions: enableCaptions,
        clips0: [] as ReturnType<typeof toSliceProjectClips>,
        clips1: toSliceProjectClips(selectedSegments),
      };

      setSavingProject(true);
      try {
        // 有项目 id → 更新；无项目 id → 新建
        const response = projectId
          ? await updateSliceProject(projectId, payload)
          : await saveSliceProject(payload);

        if (response.code !== 0) {
          toast.notify.error(response.message || '保存失败');
          return;
        }

        if (response.data.id) {
          syncProjectIdInUrl(response.data.id, { reload: false });
        }
        setDraftName(response.data.name);
        setProjectRemark(response.data.remark || nextRemark);
        setProjectTitle(response.data.title || '');
        setProjectDescription(response.data.description || '');
        setProjectTopics(response.data.topics ?? []);
        resetDirtyBaseline({
          segments: selectedSegments,
          enableCaptions,
          draftName: response.data.name,
          projectRemark: response.data.remark || nextRemark,
        });
        localStorage.setItem(DRAFT_STORAGE_KEY, response.data.name);
        setSaveModalOpen(false);
        toast.notify.success('已保存为剪辑项目', MANUAL_SLICE_SAVE_NOTIFY_DESC);
      } catch (error) {
        if (error instanceof AppError) {
          showAppError(error);
        } else {
          toast.notify.error('保存失败');
        }
      } finally {
        setSavingProject(false);
      }
    },
    [
      draftName,
      enableCaptions,
      projectId,
      projectRemark,
      resetDirtyBaseline,
      selectedSegments,
      syncProjectIdInUrl,
      video,
    ]
  );

  const handleSaveDraft = useCallback(
    async (values: { name: string; remark: string }) => {
      if (!sourceVideoId || !video) return;

      if (selectedSegments.length === 0) {
        toast.notify.warning('请先选择至少一个片段');
        return;
      }

      const { name, remark } = values;

      setSavingProject(true);
      try {
        if (saveModalMode === 'export') {
          const blob = new Blob(
            [
              JSON.stringify(
                {
                  projectName: name,
                  remark,
                  sourceVideoId,
                  projectId: projectId || undefined,
                  segments: selectedSegments,
                },
                null,
                2
              ),
            ],
            { type: 'application/json' }
          );
          const url = URL.createObjectURL(blob);
          const anchor = document.createElement('a');
          anchor.href = url;
          anchor.download = `${name}.json`;
          anchor.click();
          URL.revokeObjectURL(url);
          setSaveModalOpen(false);
          toast.notify.success('草稿已导出');
          return;
        }

        if (saveModalMode === 'create') {
          await handleSaveProject({ name, remark });
          return;
        }

        // 另存为始终走新建接口
        const response = await saveSliceProject({
          live_id: video.id,
          name,
          remark,
          project_source: 'manual',
          enable_captions: enableCaptions,
          title: projectTitle,
          description: projectDescription,
          topics: projectTopics,
          clips0: [],
          clips1: toSliceProjectClips(selectedSegments),
        });

        if (response.code !== 0) {
          toast.notify.error(response.message || '保存失败');
          return;
        }

        if (response.data.id) {
          syncProjectIdInUrl(response.data.id, { reload: false });
        }
        localStorage.setItem(DRAFT_STORAGE_KEY, response.data.name);
        setDraftName(response.data.name);
        setProjectRemark(response.data.remark || remark);
        setProjectTitle(response.data.title || projectTitle);
        setProjectDescription(response.data.description || projectDescription);
        setProjectTopics(response.data.topics ?? projectTopics);
        resetDirtyBaseline({
          segments: selectedSegments,
          enableCaptions,
          draftName: response.data.name,
          projectRemark: response.data.remark || remark,
        });
        setSaveModalOpen(false);
        toast.notify.success('已另存为新的剪辑项目', MANUAL_SLICE_SAVE_NOTIFY_DESC);
      } catch (error) {
        if (error instanceof AppError) {
          showAppError(error);
        } else {
          toast.notify.error('保存失败');
        }
      } finally {
        setSavingProject(false);
      }
    },
    [
      handleSaveProject,
      enableCaptions,
      projectId,
      projectDescription,
      projectRemark,
      projectTitle,
      projectTopics,
      resetDirtyBaseline,
      saveModalMode,
      selectedSegments,
      sourceVideoId,
      syncProjectIdInUrl,
      video,
    ]
  );

  const openSaveModal = (nextMode: 'create' | 'saveAs' | 'export') => {
    setSaveModalMode(nextMode);
    setSaveModalOpen(true);
  };

  const handleSaveClick = () => {
    if (projectTaskReadOnly) {
      toast.notify.warning('项目有进行中任务，当前仅可查看');
      return;
    }
    if (selectedSegments.length === 0) {
      toast.notify.warning('请先选择至少一个片段');
      return;
    }
    if (!projectId) {
      openSaveModal('create');
      return;
    }
    void handleSaveProject();
  };

  const handleDownloadSubtitle = useCallback(async () => {
    if (!sourceVideoId) {
      toast.notify.warning('暂无字幕文案');
      return;
    }

    setDownloadingSubtitle(true);
    try {
      const filename = `${sanitizeDownloadFilename(video?.name ?? 'subtitle')}-字幕.json`;
      await downloadSourceVideoAsrSubtitle(sourceVideoId, filename);
      toast.notify.success('字幕文件已开始下载');
    } catch (error) {
      if (error instanceof AppError) {
        showAppError(error);
      } else {
        toast.notify.error(error instanceof Error ? error.message : '字幕下载失败');
      }
    } finally {
      setDownloadingSubtitle(false);
    }
  }, [sourceVideoId, video?.name]);

  const handleSwitchToTimeline = useCallback(() => {
    confirmLeave(() => {
      if (projectId) {
        void updateSliceProject(projectId, { project_source: 'timeline' });
      }

      navigate(buildSourceVideoSliceLink(sourceVideoId, { projectId: projectId || undefined }), {
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
        projectName: draftName,
        saved: Boolean(projectId),
      }),
    [draftName, projectId]
  );

  const breadcrumbItems = useMemo(
    () =>
      buildSliceBreadcrumbItems({
        entryFrom,
        sourceVideoId,
        pageKind: 'manual',
        videoName: video?.name,
        projectName: draftName,
        saved: Boolean(projectId),
        onNavigate: handleLeaveNavigate,
      }),
    [draftName, entryFrom, handleLeaveNavigate, projectId, sourceVideoId, video?.name]
  );

  if (loading) {
    return <ManualVideoSlicePageSkeleton breadcrumbItems={breadcrumbItems} />;
  }

  if (!video) {
    return (
      <div className="slice-page slice-page_manual">
        <SlicePageHeader breadcrumbItems={breadcrumbItems} title="" />
        <div className="slice-page-empty-shell">
          <SlicePageEmptyState variant="video-unavailable" entryFrom={entryFrom} />
        </div>
      </div>
    );
  }

  return (
    <div className="slice-page slice-page_manual">
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
          <Space size={12} align="center">
            <Button
              icon={<LuDownload size={16} />}
              loading={downloadingSubtitle}
              onClick={() => void handleDownloadSubtitle()}
            >
              字幕下载
            </Button>
            <Button className="slice-mode-switch-btn" onClick={handleSwitchToTimeline}>
              切换到时间轴切片
            </Button>
          </Space>
        }
      />
      <SliceProjectTaskReadOnlyAlert
        visible={projectId != null}
        projectName={draftName}
        runningTaskCount={runningTaskCount}
      />

      {!canPreview ? (
        <div className="slice-page-empty-shell">
          <SlicePageEmptyState
            variant={streamUrl ? 'unsupported-format' : 'no-playback-url'}
            entryFrom={entryFrom}
          />
        </div>
      ) : (
        <div className="slice-editor-layout">
          <div className="slice-editor-main">
            <div ref={panelLeftRef} className="slice-editor-panel  slice-editor-panel_left">
              <div
                ref={videoBlockRef}
                className={[
                  'slice-editor-video-block',
                  videoPanelHeight != null ? 'slice-editor-video-block_customized' : '',
                ]
                  .filter(Boolean)
                  .join(' ')}
                style={
                  videoPanelHeight != null
                    ? { height: videoPanelHeight, flex: `0 0 ${videoPanelHeight}px` }
                    : undefined
                }
              >
                {/* <div className="slice-editor-panel-title">视频预览</div> */}
                <div className="slice-editor-video-shell">
                  <SliceVideoPlayer
                    ref={playerRef}
                    url={streamUrl}
                    className="slice-editor-video"
                    paragraphs={paragraphs}
                    currentTime={currentTime}
                    onSeek={handleSeek}
                    screenshotBaseName={video?.name ?? 'video-screenshot'}
                    onDurationChange={handleDurationChange}
                  />
                </div>
              </div>

              <VideoTranscriptResizeHandle
                isCustomized={videoPanelHeight != null}
                onResize={setVideoPanelHeight}
                onMeasureStart={() => videoBlockRef.current?.getBoundingClientRect().height ?? 0}
                onMeasurePanel={() => panelLeftRef.current?.getBoundingClientRect().height ?? 0}
                onReset={() => setVideoPanelHeight(null)}
              />

              <TranscriptPanel
                embedded
                paragraphs={paragraphs}
                keyword={keyword}
                onKeywordChange={setKeyword}
                onPrevMatch={() => {
                  if (!matchParagraphIds.length) return;
                  setActiveMatchIndex(
                    (activeMatchIndex - 1 + matchParagraphIds.length) % matchParagraphIds.length
                  );
                }}
                onNextMatch={() => {
                  if (!matchParagraphIds.length) return;
                  setActiveMatchIndex((activeMatchIndex + 1) % matchParagraphIds.length);
                }}
                activeParagraphId={activeParagraphId}
                transcriptHighlight={transcriptHighlight}
                selectedCopyHighlights={selectedCopyHighlights}
                isVideoPlaying={isVideoPlaying}
                activeMatchIndex={activeMatchIndex}
                matchParagraphIds={matchParagraphIds}
                onSeek={handleSeek}
                onSelectSegment={handleSelectSegment}
                readOnly={projectTaskReadOnly}
              />
            </div>
          </div>

          <SelectedCopyPanel
            segments={selectedSegments}
            paragraphs={paragraphs}
            aiSegments={aiSegments}
            currentTime={currentTime}
            activeSegmentId={activeSegmentId}
            speakers={speakers}
            maxTotalDuration={MAX_TOTAL_DURATION}
            videoDuration={videoDuration}
            submitting={submitting}
            enableCaptions={enableCaptions}
            onEnableCaptionsChange={setEnableCaptions}
            onActiveSegmentChange={setActiveSegmentId}
            onSeek={handleSeek}
            onReorder={setSelectedSegments}
            onDeleteSegment={handleDeleteSegment}
            onDeleteSelectedRange={handleDeleteSelectedRange}
            onCopySegment={handleCopySegment}
            onAdjustSegment={handleAdjustSegment}
            onAddAiSegment={handleAddAiSegment}
            onClearAll={() => {
              setSelectedSegments([]);
              setActiveSegmentId(null);
            }}
            onPreview={() => {
              if (!streamUrl) {
                toast.notify.warning('暂无可用视频，无法预览');
                return;
              }
              if (selectedSegments.length === 0) {
                toast.notify.warning('请先选择至少一个片段');
                return;
              }
              playerRef.current?.video?.pause();
              setPreviewOpen(true);
            }}
            onSave={handleSaveClick}
            savingProject={savingProject}
            onSaveAs={() => openSaveModal('saveAs')}
            onExportDraft={() => openSaveModal('export')}
            onSubmit={() => void handleSubmit()}
            readOnly={projectTaskReadOnly}
          />
        </div>
      )}

      <SegmentPreviewModal
        open={previewOpen}
        url={streamUrl}
        segments={selectedSegments}
        paragraphs={paragraphs}
        enableCaptions={enableCaptions}
        screenshotBaseName={video?.name ?? 'video-screenshot'}
        onClose={() => setPreviewOpen(false)}
      />

      <SaveDraftModal
        open={saveModalOpen}
        title={
          saveModalMode === 'export'
            ? '导出草稿'
            : saveModalMode === 'create'
              ? '保存项目'
              : '另存为项目'
        }
        defaultName={
          saveModalMode === 'saveAs'
            ? `${draftName || '人工切片'}-副本`
            : saveModalMode === 'create'
              ? buildManualProjectAutoName()
              : draftName || buildManualProjectAutoName()
        }
        defaultRemark={saveModalMode === 'saveAs' ? '' : projectRemark}
        showRemark={saveModalMode !== 'export'}
        loading={savingProject}
        onCancel={() => setSaveModalOpen(false)}
        onSubmit={(values) => void handleSaveDraft(values)}
      />
    </div>
  );
};

export default ManualVideoSlicePage;
