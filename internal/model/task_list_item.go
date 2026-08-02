package model

// TaskListItem 任务列表项。
// 全新部署下 width/height/live_url/live_name 已冗余落在 task 表，列表与详情共用 Task 字段，不再 JOIN。
type TaskListItem = Task
