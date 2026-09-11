package liveingest

import (
	"fmt"
	"path"
	"strings"

	"live-mixer/internal/pkg/storage"
)

// RecordPrefix 返回直播录像相对对象键前缀：live-record/{uuid}
func RecordPrefix(recordUUID string) string {
	uuid := strings.TrimSpace(recordUUID)
	return path.Join(storage.SubDirLiveRecord, uuid)
}

// FinalObjectKey 最终 mp4 相对键。
func FinalObjectKey(recordUUID string) string {
	return path.Join(RecordPrefix(recordUUID), "final.mp4")
}

// PlaylistObjectKey 自有播放列表相对键（媒体窗 HLS）。
func PlaylistObjectKey(recordUUID string) string {
	return path.Join(RecordPrefix(recordUUID), "live.m3u8")
}

// WindowMP4ObjectKey 媒体窗 MP4（ASR/成片权威文件）。
func WindowMP4ObjectKey(recordUUID string, windowIndex int) string {
	return path.Join(RecordPrefix(recordUUID), "windows", fmt.Sprintf("window_%05d.mp4", windowIndex))
}

// WindowTSObjectKey 媒体窗 TS（HLS 预览，与 MP4 同源）。
func WindowTSObjectKey(recordUUID string, windowIndex int) string {
	return path.Join(RecordPrefix(recordUUID), "windows", fmt.Sprintf("window_%05d.ts", windowIndex))
}

// WindowMP4FileName 本地窗 MP4 文件名。
func WindowMP4FileName(windowIndex int) string {
	return fmt.Sprintf("window_%05d.mp4", windowIndex)
}

// WindowTSFileName 本地窗 TS 文件名。
func WindowTSFileName(windowIndex int) string {
	return fmt.Sprintf("window_%05d.ts", windowIndex)
}

// SegmentObjectKey 分片相对键。
func SegmentObjectKey(recordUUID string, epoch, index int64) string {
	if epoch < 0 {
		epoch = 0
	}
	return path.Join(RecordPrefix(recordUUID), "seg", fmt.Sprintf("%d", epoch), fmt.Sprintf("seg_%05d.ts", index))
}

// SegmentStorageEpoch 抢占会增加 ingest_epoch，续录前已上传的分片仍在上一代数目录。
func SegmentStorageEpoch(claimEpoch, resumeFrom, index int64) int64 {
	epoch := claimEpoch
	if epoch < 1 {
		epoch = 1
	}
	if claimEpoch > 1 && resumeFrom > 0 && index < resumeFrom {
		return claimEpoch - 1
	}
	return epoch
}

// SegmentFileName 本地分片文件名。
func SegmentFileName(index int) string {
	return fmt.Sprintf("seg_%05d.ts", index)
}
