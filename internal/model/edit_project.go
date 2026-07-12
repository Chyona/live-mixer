package model

import (
	"time"

	"gorm.io/gorm"
)

// EditProject 剪辑项目实体。
type EditProject struct {
	ID           uint           `gorm:"primaryKey;comment:主键" json:"id"`
	CreatedBy    uint           `gorm:"not null;index;comment:创建人账号ID" json:"created_by"`
	LastEditedBy uint           `gorm:"not null;index;comment:最后编辑人账号ID" json:"last_edited_by"`
	CreatedAt    time.Time      `gorm:"comment:创建时间" json:"created_at"`
	UpdatedAt    time.Time      `gorm:"comment:最后编辑时间" json:"updated_at"`
	DeletedAt    gorm.DeletedAt `gorm:"index;comment:软删除时间" json:"-"`
}

// TableName 指定剪辑项目表名。
func (EditProject) TableName() string {
	return "edit_project"
}

// EditProjectClip 剪辑项目切片实体（每个切片为独立记录，满足第一范式）。
type EditProjectClip struct {
	ID             uint      `gorm:"primaryKey;comment:主键" json:"id"`
	ProjectID      uint      `gorm:"not null;index;comment:所属剪辑项目ID" json:"project_id"`
	LiveMaterialID uint      `gorm:"not null;index;comment:关联直播素材ID" json:"live_material_id"`
	StartMs        int64     `gorm:"not null;comment:切片开始时间毫秒" json:"start_ms"`
	EndMs          int64     `gorm:"not null;comment:切片结束时间毫秒" json:"end_ms"`
	SortOrder      int       `gorm:"not null;default:0;comment:切片排序序号" json:"sort_order"`
	CreatedAt      time.Time `gorm:"comment:创建时间" json:"created_at"`
	UpdatedAt      time.Time `gorm:"comment:更新时间" json:"updated_at"`
}

// TableName 指定剪辑项目切片表名。
func (EditProjectClip) TableName() string {
	return "edit_project_clip"
}
