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

// MasterObjectKey 前 N 窗拼接主 MP4（预览 / ASR / 成片同源）。
// 关播后同一键即为最终成片。
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

// WindowMP4ObjectKey 离散媒体窗 MP4（封窗产物；拼接 master 的输入）。
func WindowMP4ObjectKey(recordUUID string, windowIndex int) string {
	return path.Join(RecordPrefix(recordUUID), "windows", fmt.Sprintf("window_%05d.mp4", windowIndex))
}

// WindowMP4FileName 本地窗 MP4 文件名。
func WindowMP4FileName(windowIndex int) string {
	return fmt.Sprintf("window_%05d.mp4", windowIndex)
}

// WindowsDirName 本地窗目录名。
func WindowsDirName() string {
	return "windows"
}

// PlaylistObjectKey 首段封窗前的临时分片 EVENT 列表（预览不以之为准）。
func PlaylistObjectKey(recordUUID string) string {
	return path.Join(RecordPrefix(recordUUID), "live.m3u8")
}

// SegmentObjectKey 分片相对键（录制底物；封窗前的临时素材）。
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
