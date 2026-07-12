package model

import (
	"time"

	"gorm.io/gorm"
)

// LiveMaterial 直播素材实体。
type LiveMaterial struct {
	ID         uint           `gorm:"primaryKey;comment:主键" json:"id"`
	LiveURL    string         `gorm:"size:1024;not null;comment:直播链接" json:"live_url"`
	ASRResult  string         `gorm:"type:jsonb;not null;default:'{}';comment:直播视频ASR识别结果JSON" json:"asr_result"`
	CreatedBy  uint           `gorm:"not null;index;comment:添加人账号ID" json:"created_by"`
	CreatedAt  time.Time      `gorm:"comment:添加时间" json:"created_at"`
	UpdatedAt  time.Time      `gorm:"comment:最后更新时间" json:"updated_at"`
	DeletedAt  gorm.DeletedAt `gorm:"index;comment:软删除时间" json:"-"`
}

// TableName 指定直播素材表名。
func (LiveMaterial) TableName() string {
	return "live_material"
}
