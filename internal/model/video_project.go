package model

import (
	"time"

	"gorm.io/gorm"
)

// VideoProject 剪辑项目实体。
type VideoProject struct {
	ID           uint           `gorm:"primaryKey;comment:主键" json:"id"`
	CreatedBy    uint           `gorm:"not null;index;comment:创建人账号ID" json:"created_by"`
	LastEditedBy uint           `gorm:"not null;index;comment:最后编辑人账号ID" json:"last_edited_by"`
	CreatedAt    time.Time      `gorm:"comment:创建时间" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"comment:最后编辑时间" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index;comment:软删除时间" json:"-"`
}

// TableName 指定剪辑项目表名。
func (VideoProject) TableName() string {
	return "video_project"
}
