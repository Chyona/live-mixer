package model

import (
	"fmt"
	"strings"
	"time"
)

// ASR 识别状态常量（字符串标识）。
const (
	ASRStatusPending    = "pending"    // 待处理
	ASRStatusProcessing = "processing" // 识别中
	ASRStatusCompleted  = "completed"  // 已完成
	ASRStatusFailed     = "failed"     // 失败
)

// live_url / 当前主地址类型。
const (
	URLTypeFile = "file" // 使用 live_url（对象存储 mp4）
	URLTypeM3U8 = "m3u8" // 使用 m3u8_url 或跟播分片播放列表
)

// 创建时用户选择的源模式。
const (
	SourceModeUpcoming = "upcoming" // 将要直播
	SourceModeLive     = "live"     // 正在直播
	SourceModeReplay   = "replay"   // 回放 / 点播文件
)

// 推流直播生命周期状态（live_status）。
const (
	LiveStatusNone       = "none"       // 回放 / 文件，不跟播
	LiveStatusWaiting    = "waiting"    // 将要直播，等待开播窗
	LiveStatusConnecting = "connecting" // 正在直播，尚未读到媒体
	LiveStatusLive       = "live"       // 已读到媒体，分片录像中
	LiveStatusEnding     = "ending"     // 关播，正在合成 final.mp4
	LiveStatusEnded      = "ended"      // 合成成功
	LiveStatusFailed     = "failed"     // 截止前无流或不可恢复错误
)

// LiveWaitGrace 将要直播：计划开播后允许延迟开播的时长。
const LiveWaitGrace = 2 * time.Hour

// LiveConnectGrace 正在直播：创建后允许尚未读到媒体的时长。
const LiveConnectGrace = 2 * time.Hour

// LiveEarlyProbe 将要直播：计划时间前提前开始探测，减少早开丢片。
const LiveEarlyProbe = 15 * time.Minute

// LiveSegmentDurationSec 跟播分片时长（秒）。
const LiveSegmentDurationSec = 6

// LiveMediaWindowDuration 主 MP4 递增步长：每满一步将已录分片重合成一份更长的 master.mp4（10/20/30…）。
// 预览 / ASR / 一键成片始终同源该文件。
const LiveMediaWindowDuration = 10 * time.Minute

// LiveASRWindowDuration 兼容旧名：主 MP4 递增步长。
const LiveASRWindowDuration = LiveMediaWindowDuration

// MaxASRTranscribeDuration 单次送厂商转写的上限（从主 MP4 抽等长 MP3）。
// 必须先保证抽音等长：若 MP3 比视频长，厂商按时长回报后再缩放会把字幕压歪。
const MaxASRTranscribeDuration = 2 * time.Minute

// ASRSummarySegment AI 对完整 ASR 的主题分段（毫秒）。
// Title 长度宜 ≤6 字；单段时长宜在 5~60 分钟（不合规段后处理时丢弃）。
type ASRSummarySegment struct {
	Title     string `json:"title"`
	StartTime int64  `json:"start_time"`
	EndTime   int64  `json:"end_time"`
}

// ASRParagraph 全文段落划分（毫秒）；含说话人与字级时间戳。
type ASRParagraph struct {
	Speaker   string     `json:"speaker"`
	Text      string     `json:"text"`
	StartTime int64      `json:"start_time"`
	EndTime   int64      `json:"end_time"`
	Words     []ClipWord `json:"words,omitempty"`
}

// LiveMaterial 直播素材实体。
type LiveMaterial struct {
	ID     uint   `gorm:"primaryKey;comment:主键" json:"id"`
	Name   string `gorm:"size:64;not null;uniqueIndex;comment:素材名称" json:"name"`
	Remark string `gorm:"size:256;comment:备注" json:"remark"`
	// LiveURL 对象存储最终 mp4。直播类创建时预分配，INSERT 后禁止修改。回放 m3u8 可为空。
	LiveURL string `gorm:"column:live_url;size:1024;not null;default:'';comment:对象存储mp4地址" json:"live_url"`
	// M3U8URL 用户填写的 HLS 拉流地址。
	M3U8URL string `gorm:"column:m3u8_url;size:1024;not null;default:'';comment:HLS拉流地址" json:"m3u8_url"`
	// RecordUUID 直播录像对象键前缀，与 live_url 一同在创建时生成。
	RecordUUID string `gorm:"column:record_uuid;size:64;not null;default:'';comment:录像对象键UUID" json:"record_uuid"`
	// RecordPlaylistURL 自有分片播放列表（跟播过程中可更新）。
	RecordPlaylistURL string `gorm:"column:record_playlist_url;size:2048;not null;default:'';comment:自有HLS播放列表" json:"record_playlist_url"`
	// URLType 当前成片/点播主地址：m3u8 或 file。
	URLType string `gorm:"column:url_type;size:16;not null;default:file;comment:当前主地址file或m3u8" json:"url_type"`
	// SourceMode 创建模式：upcoming/live/replay。
	SourceMode string `gorm:"column:source_mode;size:16;not null;default:replay;comment:upcoming/live/replay" json:"source_mode"`
	// LiveStatus 跟播生命周期。
	LiveStatus        string              `gorm:"column:live_status;size:16;not null;default:none;index;comment:跟播状态" json:"live_status"`
	ScheduledAt       *time.Time          `gorm:"column:scheduled_at;comment:计划开播时间" json:"scheduled_at,omitempty"`
	WaitDeadlineAt    *time.Time          `gorm:"column:wait_deadline_at;comment:将要直播开播截止" json:"wait_deadline_at,omitempty"`
	ConnectDeadlineAt *time.Time          `gorm:"column:connect_deadline_at;comment:正在直播连上截止" json:"connect_deadline_at,omitempty"`
	StreamStartedAt   *time.Time          `gorm:"column:stream_started_at;comment:第一次读到媒体的时间" json:"stream_started_at,omitempty"`
	ASRCursorMS       int64               `gorm:"column:asr_cursor_ms;not null;default:0;comment:ASR已覆盖毫秒" json:"asr_cursor_ms"`
	IngestEpoch       int64               `gorm:"column:ingest_epoch;not null;default:0;comment:录像/收尾抢占代数" json:"ingest_epoch"`
	ASREpoch          int64               `gorm:"column:asr_epoch;not null;default:0;comment:窗口ASR抢占代数" json:"asr_epoch"`
	NextSeg           int64               `gorm:"column:next_seg;not null;default:0;comment:下一分片序号" json:"next_seg"`
	LastHeartbeatAt   *time.Time          `gorm:"column:last_heartbeat_at;comment:录像/收尾心跳" json:"last_heartbeat_at,omitempty"`
	ASRHeartbeatAt    *time.Time          `gorm:"column:asr_heartbeat_at;comment:窗口ASR心跳" json:"asr_heartbeat_at,omitempty"`
	LastProgressAt    *time.Time          `gorm:"column:last_progress_at;comment:最近一次分片进度时间" json:"last_progress_at,omitempty"`
	ASRDue            bool                `gorm:"column:asr_due;not null;default:false;comment:是否有待跑窗口ASR" json:"asr_due"`
	IngestResumeSeg   int64               `gorm:"column:ingest_resume_seg;not null;default:0;comment:本场录像起始分片序号" json:"ingest_resume_seg"`
	IngestErrorMsg    string              `gorm:"column:ingest_error_msg;type:text;comment:跟播失败原因" json:"ingest_error_msg,omitempty"`
	// MediaWindowMS 主 MP4 递增步长（毫秒）；创建时写入，默认 LiveMediaWindowDuration。
	MediaWindowMS int64 `gorm:"column:media_window_ms;not null;default:0;comment:主MP4递增步长毫秒" json:"media_window_ms"`
	// MediaWindows 主 MP4 元数据 JSON（至多一条：url/时长/已封分片上界）。
	MediaWindows string `gorm:"column:media_windows;type:jsonb;not null;default:'[]';comment:主MP4元数据JSON" json:"media_windows"`
	// NextWindowSeg 下一未封入主 MP4 的起始分片下标。
	NextWindowSeg int64 `gorm:"column:next_window_seg;not null;default:0;comment:下一未封入主MP4分片" json:"next_window_seg"`
	LiveASR           string              `gorm:"column:live_asr;type:jsonb;not null;default:'{}';comment:直播视频ASR识别结果JSON" json:"live_asr"`
	ASRSummaries      []ASRSummarySegment `gorm:"column:asr_summaries;serializer:json;type:jsonb;not null;default:'[]';comment:AI主题分段" json:"asr_summaries"`
	ASRParagraphs     []ASRParagraph      `gorm:"column:asr_paragraphs;serializer:json;type:jsonb;not null;default:'[]';comment:全文段落划分" json:"asr_paragraphs"`
	Duration          int64               `gorm:"not null;default:0;comment:本地跟播录像真实时长毫秒" json:"duration"`
	Width             int                 `gorm:"not null;default:0;comment:直播画面宽度像素" json:"width"`
	Height            int                 `gorm:"not null;default:0;comment:直播画面高度像素" json:"height"`
	ASRStatus         string              `gorm:"column:asr_status;size:20;not null;default:pending;index;comment:ASR识别状态" json:"asr_status"`
	ASRProgress       int16               `gorm:"column:asr_progress;not null;default:0;comment:ASR识别进度0到100" json:"asr_progress"`
	ASRErrorMsg       string              `gorm:"column:asr_error_msg;type:text;comment:ASR识别失败原因" json:"asr_error_msg,omitempty"`
	ASRStartedAt      *time.Time          `gorm:"column:asr_started_at;comment:ASR识别开始时间" json:"asr_started_at,omitempty"`
	ASRUpdatedAt      *time.Time          `gorm:"column:asr_updated_at;comment:ASR识别状态最后更新时间" json:"asr_updated_at,omitempty"`
	ASRCompletedAt    *time.Time          `gorm:"column:asr_completed_at;comment:ASR识别完成时间" json:"asr_completed_at,omitempty"`
	ASRVersion        int64               `gorm:"column:asr_version;not null;default:0;comment:ASR乐观锁版本号" json:"asr_version"`
	CreatedBy         uint                `gorm:"not null;index;comment:添加人账号ID" json:"created_by"`
	CreatedAt         time.Time           `gorm:"comment:添加时间" json:"created_at"`
	UpdatedAt         time.Time           `gorm:"comment:最后更新时间" json:"updated_at"`
	Ext               string              `gorm:"size:1024;comment:扩展字段" json:"ext"`
}

// TableName 指定直播素材表名。
func (LiveMaterial) TableName() string {
	return "live_material"
}

// IsReplaySource 回放 / 文件，不走跟播引擎。
func (m *LiveMaterial) IsReplaySource() bool {
	if m == nil {
		return true
	}
	return m.SourceMode == SourceModeReplay || m.SourceMode == "" || m.LiveStatus == LiveStatusNone
}

// NeedsLiveIngest 将要直播或正在直播（含失败后可重试）。
func (m *LiveMaterial) NeedsLiveIngest() bool {
	if m == nil {
		return false
	}
	return m.SourceMode == SourceModeUpcoming || m.SourceMode == SourceModeLive
}

// CanUpdateM3U8 等待/连接/失败阶段允许刷新拉流地址。
func (m *LiveMaterial) CanUpdateM3U8() bool {
	if m == nil {
		return false
	}
	switch m.LiveStatus {
	case LiveStatusWaiting, LiveStatusConnecting, LiveStatusFailed:
		return true
	default:
		return false
	}
}

// PlayURL 前端播放地址。
// 跟播/收尾/关播：只用已就绪的递增主 MP4（media_windows[0].url），绝不回退到源站滑动 m3u8。
// 主 MP4 未封出前返回空（创建时预分配的 live_url 尚无对象内容）。
// 回放素材：用户 m3u8 / live_url。
func (m *LiveMaterial) PlayURL() string {
	if m == nil {
		return ""
	}
	if master, ok := m.ParsedMediaWindows().Master(); ok {
		if u := strings.TrimSpace(master.URL); u != "" {
			return u
		}
	}
	switch m.LiveStatus {
	case LiveStatusWaiting, LiveStatusConnecting, LiveStatusLive, LiveStatusEnding:
		return ""
	case LiveStatusEnded:
		if u := strings.TrimSpace(m.LiveURL); u != "" {
			return u
		}
		return ""
	}
	if m.URLType == URLTypeFile {
		return strings.TrimSpace(m.LiveURL)
	}
	if m.IsReplaySource() {
		if u := strings.TrimSpace(m.M3U8URL); u != "" {
			return u
		}
		return strings.TrimSpace(m.LiveURL)
	}
	return ""
}

// MasterReadyMS 主 MP4 已就绪时长（毫秒）。
func (m *LiveMaterial) MasterReadyMS() int64 {
	if m == nil {
		return 0
	}
	return m.ParsedMediaWindows().TotalReadyMS()
}

// EffectiveMediaWindowMS 本素材媒体窗毫秒。
func (m *LiveMaterial) EffectiveMediaWindowMS() int64 {
	if m != nil && m.MediaWindowMS > 0 {
		return m.MediaWindowMS
	}
	ms := int64(LiveMediaWindowDuration / time.Millisecond)
	if ms <= 0 {
		ms = 10 * 60 * 1000
	}
	return ms
}

// ParsedMediaWindows 解析媒体窗列表。
func (m *LiveMaterial) ParsedMediaWindows() MediaWindowList {
	if m == nil {
		return nil
	}
	return ParseMediaWindows(m.MediaWindows)
}

// ProcessMediaURL ASR（回放）与成片在 url_type=file 或回放 m3u8 时使用的媒体地址。
func (m *LiveMaterial) ProcessMediaURL() string {
	if m == nil {
		return ""
	}
	if m.URLType == URLTypeFile && strings.TrimSpace(m.LiveURL) != "" {
		return m.LiveURL
	}
	if u := strings.TrimSpace(m.M3U8URL); u != "" {
		return u
	}
	return strings.TrimSpace(m.LiveURL)
}

// HasRecordSegments 是否已有跟播分片（可用于直播中裁剪）。
func (m *LiveMaterial) HasRecordSegments() bool {
	return m != nil && m.NextSeg > 0 && strings.TrimSpace(m.RecordUUID) != ""
}

// ASRCoversClips 选区是否已落在已转写范围内（完成态或直播窗口 ASR）。
func (m *LiveMaterial) ASRCoversClips(clips []ClipRange) bool {
	if m == nil {
		return false
	}
	if m.ASRStatus == ASRStatusCompleted {
		return true
	}
	if !m.NeedsLiveIngest() {
		return false
	}
	switch m.LiveStatus {
	case LiveStatusLive, LiveStatusEnding, LiveStatusEnded:
	default:
		return false
	}
	var maxEnd int64
	for _, c := range clips {
		if c.EndTime > maxEnd {
			maxEnd = c.EndTime
		}
	}
	return maxEnd > 0 && maxEnd <= m.ASRCursorMS
}

// FormatClockMS 将毫秒格式化为 m:ss 或 h:mm:ss（与前端 formatVideoDuration 对齐）。
func FormatClockMS(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	totalSec := ms / 1000
	h := totalSec / 3600
	m := (totalSec % 3600) / 60
	s := totalSec % 60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%d:%02d", m, s)
}
