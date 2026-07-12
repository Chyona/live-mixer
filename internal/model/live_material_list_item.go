package model

import (
	"time"
)

// LiveMaterialListItem 直播素材列表项，不含 live_asr，用于列表接口减轻响应体积。
type LiveMaterialListItem struct {
	ID           uint       `gorm:"primaryKey" json:"id"`
	Name         string     `gorm:"size:64;not null" json:"name"`
	Remark       string     `gorm:"size:256" json:"remark"`
	LiveURL      string     `gorm:"size:1024;not null" json:"live_url"`
	Duration     int64      `gorm:"not null;default:0" json:"duration"`
	ASRStatus    string     `gorm:"column:asr_status;size:20;not null;default:pending" json:"asr_status"`
	ASRProgress  int16      `gorm:"column:asr_progress;not null;default:0" json:"asr_progress"`
	ASRErrorMsg  string     `gorm:"column:asr_error_msg;type:text" json:"asr_error_msg,omitempty"`
	ASRStartedAt *time.Time `gorm:"column:asr_started_at" json:"asr_started_at,omitempty"`
	ASRUpdatedAt *time.Time `gorm:"column:asr_updated_at" json:"asr_updated_at,omitempty"`
	CreatedBy    uint       `gorm:"not null" json:"created_by"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	Ext          string     `gorm:"size:1024" json:"ext"`
}

// TableName 指定直播素材表名，与 LiveMaterial 共用同一张表。
func (LiveMaterialListItem) TableName() string {
	return "live_material"
}
