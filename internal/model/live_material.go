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

// LiveMaterial 直播素材实体。
type LiveMaterial struct {
	ID           uint       `gorm:"primaryKey;comment:主键" json:"id"`
	Name         string     `gorm:"size:64;not null;comment:素材名称" json:"name"`
	Remark       string     `gorm:"size:256;comment:备注" json:"remark"`
	LiveURL      string     `gorm:"size:1024;not null;comment:直播链接" json:"live_url"`
	LiveASR      string     `gorm:"column:live_asr;type:jsonb;not null;default:'{}';comment:直播视频ASR识别结果JSON" json:"live_asr"`
	Duration     int64      `gorm:"not null;default:0;comment:直播时长毫秒" json:"duration"`
	ASRStatus    string     `gorm:"column:asr_status;size:20;not null;default:pending;index;comment:ASR识别状态" json:"asr_status"`
	ASRProgress  int16      `gorm:"column:asr_progress;not null;default:0;comment:ASR识别进度0到100" json:"asr_progress"`
	ASRErrorMsg  string     `gorm:"column:asr_error_msg;type:text;comment:ASR识别失败原因" json:"asr_error_msg,omitempty"`
	ASRStartedAt *time.Time `gorm:"column:asr_started_at;comment:ASR识别开始时间" json:"asr_started_at,omitempty"`
	ASRUpdatedAt *time.Time `gorm:"column:asr_updated_at;comment:ASR识别状态最后更新时间" json:"asr_updated_at,omitempty"`
	CreatedBy    uint       `gorm:"not null;index;comment:添加人账号ID" json:"created_by"`
	CreatedAt    time.Time  `gorm:"comment:添加时间" json:"created_at"`
	UpdatedAt    time.Time  `gorm:"comment:最后更新时间" json:"updated_at"`
	Ext          string     `gorm:"size:1024;comment:扩展字段" json:"ext"`
}

// TableName 指定直播素材表名。
func (LiveMaterial) TableName() string {
	return "live_material"
}
