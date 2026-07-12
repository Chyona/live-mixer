package model

import (
	"time"
)

// LiveMaterial 直播素材实体。
type LiveMaterial struct {
	ID        uint      `gorm:"primaryKey;comment:主键" json:"id"`
	Name      string    `gorm:"size:64;comment:素材名称" json:"name"`
	Remark    string    `gorm:"size:256;comment:备注" json:"remark"`
	LiveURL   string    `gorm:"size:1024;not null;comment:直播链接" json:"live_url"`
	LiveASR   string    `gorm:"column:live_asr;type:jsonb;not null;default:'{}';comment:直播视频ASR识别结果JSON" json:"live_asr"`
	Duration  int64     `gorm:"not null;default:0;comment:直播时长毫秒" json:"duration"`
	CreatedBy uint      `gorm:"not null;index;comment:添加人账号ID" json:"created_by"`
	CreatedAt time.Time `gorm:"comment:添加时间" json:"created_at"`
	UpdatedAt time.Time `gorm:"comment:最后更新时间" json:"updated_at"`
}

// TableName 指定直播素材表名。
func (LiveMaterial) TableName() string {
	return "live_material"
}
