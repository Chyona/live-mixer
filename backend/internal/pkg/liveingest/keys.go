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

// MasterObjectKey 跟播递增主 MP4（10/20/30… 分钟），预览 / ASR / 成片同源。
// 关播后同一键即为最终成片（不再另存 window_N / 再拼 final）。
func MasterObjectKey(recordUUID string) string {
	return path.Join(RecordPrefix(recordUUID), "master.mp4")
}

// FinalObjectKey 兼容旧名：与 MasterObjectKey 相同。
func FinalObjectKey(recordUUID string) string {
	return MasterObjectKey(recordUUID)
}

// MasterMP4FileName 本地主 MP4 文件名。
func MasterMP4FileName() string {
	return "master.mp4"
}

// PlaylistObjectKey 首段封主 MP4 前的临时分片 EVENT 列表（预览不以之为准）。
func PlaylistObjectKey(recordUUID string) string {
	return path.Join(RecordPrefix(recordUUID), "live.m3u8")
}

// SegmentObjectKey 分片相对键（录制底物；封主 MP4 前的临时素材）。
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
