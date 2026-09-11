package service

import (
	"errors"
	"strings"
	"time"

	"live-mixer/internal/model"
	"live-mixer/internal/pkg/asr"
	"live-mixer/internal/pkg/liveingest"
	"live-mixer/internal/pkg/media"

	"github.com/google/uuid"
)

// CreateLiveMaterialInput 创建直播素材入参。
type CreateLiveMaterialInput struct {
	Name        string
	SourceMode  string
	SourceURL   string
	ScheduledAt *time.Time
	Remark      string
	Ext         string
}

// LiveRecordURLAllocator 预分配不可变 live_url（对象存储直链）。
type LiveRecordURLAllocator interface {
	PublicLiveURL(recordUUID string) string
}

type staticLiveURLAllocator struct{}

func (staticLiveURLAllocator) PublicLiveURL(recordUUID string) string {
	return "https://live-mixer.invalid/" + liveingest.FinalObjectKey(recordUUID)
}

var (
	ErrInvalidSourceMode     = errors.New("source_mode 无效，应为 upcoming / live / replay")
	ErrScheduledAtRequired   = errors.New("将要直播必须选择计划开播时间")
	ErrScheduledAtExpired    = errors.New("计划开播已超过 2 小时容错窗口，请改选正在直播或更新时间")
	ErrUpcomingRequiresM3U8  = errors.New("将要直播 / 正在直播必须填写 m3u8 地址")
	ErrInvalidSourceURL      = errors.New("请输入有效的 http/https 地址")
	ErrIngestRetryOnlyFailed = errors.New("仅跟播失败的素材可重试")
)

func normalizeSourceMode(mode, sourceURL string) (string, error) {
	mode = strings.ToLower(strings.TrimSpace(mode))
	if mode == "" {
		if media.IsM3U8URL(sourceURL) {
			return model.SourceModeReplay, nil
		}
		return model.SourceModeReplay, nil
	}
	switch mode {
	case model.SourceModeUpcoming, model.SourceModeLive, model.SourceModeReplay:
		return mode, nil
	default:
		return "", ErrInvalidSourceMode
	}
}

func isValidHTTPURL(raw string) bool {
	s := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

func buildCreateMaterial(createdBy uint, in CreateLiveMaterialInput, alloc LiveRecordURLAllocator) (*model.LiveMaterial, error) {
	name := strings.TrimSpace(in.Name)
	sourceURL := strings.TrimSpace(in.SourceURL)
	if name == "" {
		return nil, errors.New("素材名称不能为空")
	}
	if sourceURL == "" {
		return nil, errors.New("直播链接不能为空")
	}
	if !isValidHTTPURL(sourceURL) {
		return nil, ErrInvalidSourceURL
	}
	mode, err := normalizeSourceMode(in.SourceMode, sourceURL)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	material := &model.LiveMaterial{
		MediaWindowMS: int64(model.LiveMediaWindowDuration / time.Millisecond),
		MediaWindows:  "[]",
		Name:          name,
		Remark:        in.Remark,
		Ext:           in.Ext,
		SourceMode:    mode,
		LiveASR:       "{}",
		ASRSummaries:  []model.ASRSummarySegment{},
		ASRParagraphs: []model.ASRParagraph{},
		ASRStatus:     model.ASRStatusPending,
		CreatedBy:     createdBy,
	}

	switch mode {
	case model.SourceModeReplay:
		if media.IsM3U8URL(sourceURL) {
			material.M3U8URL = sourceURL
			material.URLType = model.URLTypeM3U8
			material.LiveStatus = model.LiveStatusNone
			material.LiveURL = ""
		} else {
			if _, err := asr.DetectFormat(sourceURL); err != nil {
				if strings.Contains(err.Error(), "不支持的") {
					return nil, ErrUnsupportedMediaFormat
				}
				return nil, err
			}
			material.LiveURL = sourceURL
			material.URLType = model.URLTypeFile
			material.LiveStatus = model.LiveStatusNone
		}
	case model.SourceModeUpcoming, model.SourceModeLive:
		if !media.IsM3U8URL(sourceURL) {
			return nil, ErrUpcomingRequiresM3U8
		}
		if alloc == nil {
			alloc = staticLiveURLAllocator{}
		}
		recordUUID := strings.ReplaceAll(uuid.NewString(), "-", "")
		material.RecordUUID = recordUUID
		material.LiveURL = alloc.PublicLiveURL(recordUUID)
		material.M3U8URL = sourceURL
		material.URLType = model.URLTypeM3U8
		if mode == model.SourceModeUpcoming {
			if in.ScheduledAt == nil || in.ScheduledAt.IsZero() {
				return nil, ErrScheduledAtRequired
			}
			scheduled := in.ScheduledAt.UTC()
			deadline := scheduled.Add(model.LiveWaitGrace)
			if !now.Before(deadline) {
				return nil, ErrScheduledAtExpired
			}
			material.ScheduledAt = &scheduled
			material.WaitDeadlineAt = &deadline
			material.LiveStatus = model.LiveStatusWaiting
		} else {
			deadline := now.Add(model.LiveConnectGrace)
			material.ConnectDeadlineAt = &deadline
			material.LiveStatus = model.LiveStatusConnecting
		}
	}
	return material, nil
}
