package op

import (
	"context"
	"fmt"
	"time"

	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
)

// VideoTaskCreate 登记一个视频任务与承接它的上游成员。
// 同一任务 ID 重复登记时更新承接方, 使重试创建不会留下指向旧成员的记录。
func VideoTaskCreate(task model.VideoTask, ctx context.Context) error {
	if task.TaskID == "" {
		return fmt.Errorf("video task id is required")
	}
	existing, err := VideoTaskGet(task.TaskID, ctx)
	if err == nil {
		if err := db.GetDB().WithContext(ctx).Model(&model.VideoTask{}).
			Where("id = ?", existing.ID).
			Updates(map[string]any{
				"model":            task.Model,
				"channel_id":       task.ChannelID,
				"channel_key_id":   task.ChannelKeyID,
				"channel_model_id": task.ChannelModelID,
				"channel_model":    task.ChannelModel,
				"api_key_id":       task.APIKeyID,
				"status":           task.Status,
				"updated_at":       time.Now(),
			}).Error; err != nil {
			return err
		}
		return nil
	}
	if err := db.GetDB().WithContext(ctx).Create(&task).Error; err != nil {
		return err
	}
	videoTaskPrune(ctx)
	return nil
}

// videoTaskPrune 清掉过期的任务登记: 上游的任务本身很快失效, 留着既查不到也占地方。
func videoTaskPrune(ctx context.Context) {
	deadline := time.Now().Add(-7 * 24 * time.Hour)
	db.GetDB().WithContext(ctx).
		Where("created_at < ?", deadline).
		Delete(&model.VideoTask{})
}

// VideoTaskGet 按任务 ID 取出视频任务及其承接的上游成员。
func VideoTaskGet(taskID string, ctx context.Context) (model.VideoTask, error) {
	var task model.VideoTask
	if err := db.GetDB().WithContext(ctx).Where("task_id = ?", taskID).First(&task).Error; err != nil {
		return model.VideoTask{}, err
	}
	return task, nil
}

// VideoTaskUpdateStatus 记录上游最近一次回报的任务状态。
func VideoTaskUpdateStatus(taskID, status string, ctx context.Context) error {
	if status == "" {
		return nil
	}
	return db.GetDB().WithContext(ctx).Model(&model.VideoTask{}).
		Where("task_id = ?", taskID).
		Updates(map[string]any{"status": status, "updated_at": time.Now()}).Error
}

// VideoTaskDelete 删除视频任务的登记; 任务已在上游删除时调用。
func VideoTaskDelete(taskID string, ctx context.Context) error {
	return db.GetDB().WithContext(ctx).Where("task_id = ?", taskID).Delete(&model.VideoTask{}).Error
}
