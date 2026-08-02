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
	URLType      string     `gorm:"column:url_type;size:16;not null;default:file" json:"url_type"`
	LiveStatus   string     `gorm:"column:live_status;size:16;not null;default:none" json:"live_status"`
	Duration     int64      `gorm:"not null;default:0" json:"duration"`
	Width        int        `gorm:"not null;default:0" json:"width"`
	Height       int        `gorm:"not null;default:0" json:"height"`
	ASRStatus    string     `gorm:"column:asr_status;size:20;not null;default:pending" json:"asr_status"`
	ASRProgress  int16      `gorm:"column:asr_progress;not null;default:0" json:"asr_progress"`
	ASRErrorMsg  string     `gorm:"column:asr_error_msg;type:text" json:"asr_error_msg,omitempty"`
	ASRStartedAt   *time.Time `gorm:"column:asr_started_at" json:"asr_started_at,omitempty"`
	ASRUpdatedAt   *time.Time `gorm:"column:asr_updated_at" json:"asr_updated_at,omitempty"`
	ASRCompletedAt *time.Time `gorm:"column:asr_completed_at" json:"asr_completed_at,omitempty"`
	ASRVersion     int64      `gorm:"column:asr_version;not null;default:0" json:"asr_version"`
	CreatedBy    uint       `gorm:"not null" json:"created_by"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
	Ext          string     `gorm:"size:1024" json:"ext"`
	// ProjectCount 关联 video_project 数量（列表查询时由子查询填充，非表字段）。
	ProjectCount int64 `gorm:"->" json:"project_count"`
	// MatchedParagraphs asr_keywords 命中的 asr_paragraphs 段落（仅有关键词时填充）。
	MatchedParagraphs []ASRParagraph `gorm:"-" json:"matched_paragraphs,omitempty"`
}

// TableName 指定直播素材表名，与 LiveMaterial 共用同一张表。
func (LiveMaterialListItem) TableName() string {
	return "live_material"
}
