package model

import (
	"time"
)

// ASR 识别状态常量（字符串标识）。
const (
	ASRStatusPending    = "pending"    // 待处理
	ASRStatusProcessing = "processing" // 识别中
	ASRStatusCompleted  = "completed"  // 已完成
	ASRStatusFailed     = "failed"     // 失败
)

// live_url 类型常量。
const (
	URLTypeFile = "file" // 音视频文件
	URLTypeM3U8 = "m3u8" // HLS 流媒体
)

// ASRSummarySegment AI 对完整 ASR 的总结与分段（毫秒）。
// Title 长度宜 ≤6 字；单段总结对应时长宜在 5~60 分钟。
type ASRSummarySegment struct {
	Title     string `json:"title"`
	Summary   string `json:"summary"`
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
	ID           uint       `gorm:"primaryKey;comment:主键" json:"id"`
	Name         string     `gorm:"size:64;not null;uniqueIndex;comment:素材名称" json:"name"`
	Remark       string     `gorm:"size:256;comment:备注" json:"remark"`
	LiveURL      string     `gorm:"size:1024;not null;uniqueIndex;comment:直播链接" json:"live_url"`
	// URLType live_url 类型：file=音视频文件，m3u8=HLS 流。
	URLType      string     `gorm:"column:url_type;size:16;not null;default:file;comment:直播链接类型file或m3u8" json:"url_type"`
	LiveASR      string     `gorm:"column:live_asr;type:jsonb;not null;default:'{}';comment:直播视频ASR识别结果JSON" json:"live_asr"`
	// ASRSummaries AI 总结分段；ASRParagraphs 全文段落划分。默认空数组。
	ASRSummaries  []ASRSummarySegment `gorm:"column:asr_summaries;serializer:json;type:jsonb;not null;default:'[]';comment:AI总结分段" json:"asr_summaries"`
	ASRParagraphs []ASRParagraph      `gorm:"column:asr_paragraphs;serializer:json;type:jsonb;not null;default:'[]';comment:全文段落划分" json:"asr_paragraphs"`
	Duration     int64      `gorm:"not null;default:0;comment:直播时长毫秒" json:"duration"`
	// Width / Height 直播画面分辨率（像素）；0 表示未知。
	Width        int        `gorm:"not null;default:0;comment:直播画面宽度像素" json:"width"`
	Height       int        `gorm:"not null;default:0;comment:直播画面高度像素" json:"height"`
	ASRStatus    string     `gorm:"column:asr_status;size:20;not null;default:pending;index;comment:ASR识别状态" json:"asr_status"`
	ASRProgress  int16      `gorm:"column:asr_progress;not null;default:0;comment:ASR识别进度0到100" json:"asr_progress"`
	ASRErrorMsg  string     `gorm:"column:asr_error_msg;type:text;comment:ASR识别失败原因" json:"asr_error_msg,omitempty"`
	ASRStartedAt   *time.Time `gorm:"column:asr_started_at;comment:ASR识别开始时间" json:"asr_started_at,omitempty"`
	ASRUpdatedAt   *time.Time `gorm:"column:asr_updated_at;comment:ASR识别状态最后更新时间" json:"asr_updated_at,omitempty"`
	ASRCompletedAt *time.Time `gorm:"column:asr_completed_at;comment:ASR识别完成时间" json:"asr_completed_at,omitempty"`
	// ASRVersion ASR 乐观锁版本号：抢占 pending→processing 时 CAS 递增。
	ASRVersion int64 `gorm:"column:asr_version;not null;default:0;comment:ASR乐观锁版本号" json:"asr_version"`
	CreatedBy    uint       `gorm:"not null;index;comment:添加人账号ID" json:"created_by"`
	CreatedAt    time.Time  `gorm:"comment:添加时间" json:"created_at"`
	UpdatedAt    time.Time  `gorm:"comment:最后更新时间" json:"updated_at"`
	Ext          string     `gorm:"size:1024;comment:扩展字段" json:"ext"`
}

// TableName 指定直播素材表名。
func (LiveMaterial) TableName() string {
	return "live_material"
}
