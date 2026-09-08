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

// PlaylistObjectKey 自有播放列表相对键。
func PlaylistObjectKey(recordUUID string) string {
	return path.Join(RecordPrefix(recordUUID), "live.m3u8")
}

// SegmentObjectKey 分片相对键。
func SegmentObjectKey(recordUUID string, epoch, index int64) string {
	return path.Join(RecordPrefix(recordUUID), "seg", fmt.Sprintf("%d", epoch), fmt.Sprintf("seg_%05d.ts", index))
}

// SegmentFileName 本地分片文件名。
func SegmentFileName(index int) string {
	return fmt.Sprintf("seg_%05d.ts", index)
}
