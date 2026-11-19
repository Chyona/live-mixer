package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"time"
)

const (
	windowFingerprintHeadBytes = 1 << 20 // 1 MiB
	windowFingerprintTailBytes = 64 << 10 // 64 KiB
)

// windowFileFingerprint 快速指纹：size + 头 1MiB + 尾 64KiB 的 SHA256。
// 用于检测直播变回放后重复 remux 的同一窗内容。
func windowFileFingerprint(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	size := st.Size()
	if size <= 0 {
		return "", fmt.Errorf("empty file")
	}

	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	_, _ = fmt.Fprintf(h, "size:%d\n", size)

	headN := int64(windowFingerprintHeadBytes)
	if headN > size {
		headN = size
	}
	if _, err := io.Copy(h, io.LimitReader(f, headN)); err != nil {
		return "", err
	}

	if size > headN {
		tailN := int64(windowFingerprintTailBytes)
		if tailN > size-headN {
			tailN = size - headN
		}
		if _, err := f.Seek(size-tailN, io.SeekStart); err != nil {
			return "", err
		}
		if _, err := io.Copy(h, io.LimitReader(f, tailN)); err != nil {
			return "", err
		}
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// isFastVODRemux 墙钟远小于目标窗长却产出满窗媒体：典型「直播变 VOD 后 ffmpeg 秒级 remux」。
func isFastVODRemux(elapsed time.Duration, probedMS int64, windowSec int, fullWindowMS int64) bool {
	if windowSec <= 0 || probedMS < fullWindowMS || fullWindowMS <= 0 {
		return false
	}
	threshold := 45 * time.Second
	if d := time.Duration(windowSec) * time.Second * 15 / 100; d > threshold {
		threshold = d
	}
	return elapsed > 0 && elapsed < threshold
}

// isRealtimeWindowElapsed 本窗墙钟达到目标窗长一半，视为真实直播节奏。
func isRealtimeWindowElapsed(elapsed time.Duration, windowSec int) bool {
	if windowSec <= 0 || elapsed <= 0 {
		return false
	}
	return elapsed >= time.Duration(windowSec)*time.Second/2
}

// replayEndSignal 在已见过实时窗后，判定本窗是否呈现「变回放」信号。
func replayEndSignal(sawRealtime bool, hasEndList bool, elapsed time.Duration, probedMS int64, windowSec int, fullWindowMS int64) (hit bool, reason string) {
	if !sawRealtime {
		return false, ""
	}
	if hasEndList {
		return true, "endlist"
	}
	if isFastVODRemux(elapsed, probedMS, windowSec, fullWindowMS) {
		return true, "fast_remux"
	}
	return false, ""
}
