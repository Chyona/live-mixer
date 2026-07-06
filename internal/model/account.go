// Package model 定义 GORM 数据实体，供 repository 与 envinit 共享。
package model

import (
	"time"

	"gorm.io/gorm"
)

// Account 账号实体模型。
type Account struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	Username  string         `gorm:"size:64;uniqueIndex;not null" json:"username"`
	Email     string         `gorm:"size:128;uniqueIndex;not null" json:"email"`
	Password  string         `gorm:"size:255;not null" json:"-"`
	Nickname  string         `gorm:"size:64" json:"nickname"`
	Avatar    string         `gorm:"size:512" json:"avatar"`                       // 用户头像 URL
	Roles     string         `gorm:"size:64" json:"roles"`                         // 用户角色，多个角色用逗号分隔
	OpenID    string         `gorm:"column:open_id;size:128;index" json:"open_id"` // 第三方授权 OpenId
	Remark    string         `gorm:"size:256" json:"remark"`                       // 备注
	Phone     string         `gorm:"size:32" json:"phone"`                         // 手机号码
	Ext       string         `gorm:"size:1024" json:"ext"`                         // 扩展字段
	Status    int8           `gorm:"default:1;comment:1正常 0禁用" json:"status"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

// TableName 指定账号表名。
func (Account) TableName() string {
	return "account"
}
