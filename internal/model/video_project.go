package model

import (
	"time"
)

// VideoProject 剪辑项目实体。
type VideoProject struct {
	ID        uint      `gorm:"primaryKey;comment:主键" json:"id"`
	Name      string    `gorm:"size:64;comment:项目名称" json:"name"`
	Remark    string    `gorm:"size:256;comment:备注" json:"remark"`
	CreatedBy uint      `gorm:"not null;index;comment:创建人账号ID" json:"created_by"`
	CreatedAt time.Time `gorm:"comment:创建时间" json:"created_at"`
	UpdatedAt time.Time `gorm:"comment:最后编辑时间" json:"updated_at"`
}

// TableName 指定剪辑项目表名。
func (VideoProject) TableName() string {
	return "video_project"
}
