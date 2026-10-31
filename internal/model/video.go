package model

import "time"

// VideoTask 记录一个视频生成任务与承接它的上游成员。
// 视频生成是异步的: 创建只拿到任务 ID, 之后查询与删除都要回到当初承接该任务的渠道与凭据,
// 故把这条对应关系落库, 由任务 ID 找回上游。
type VideoTask struct {
	ID             int       `json:"id" gorm:"primaryKey"`                 // 主键。
	TaskID         string    `json:"task_id" gorm:"unique;not null;index"` // 上游返回的任务 ID, 也是客户端查询与删除用的 ID。
	Model          string    `json:"model" gorm:"not null"`                // 客户端请求的分组名称。
	ChannelID      int       `json:"channel_id" gorm:"not null;index"`     // 承接该任务的渠道 ID。
	ChannelKeyID   int       `json:"channel_key_id" gorm:"not null"`       // 承接该任务的凭据 ID。
	ChannelModelID int       `json:"channel_model_id" gorm:"not null"`     // 承接该任务的渠道模型 ID。
	ChannelModel   string    `json:"channel_model" gorm:"not null"`        // 承接该任务的渠道模型名称。
	APIKeyID       int       `json:"api_key_id"`                           // 创建该任务的 API Key ID。
	Status         string    `json:"status"`                               // 上游最近一次回报的任务状态。
	CreatedAt      time.Time `json:"created_at" gorm:"autoCreateTime"`     // 创建时间。
	UpdatedAt      time.Time `json:"updated_at" gorm:"autoUpdateTime"`     // 最近一次更新时间。
}
