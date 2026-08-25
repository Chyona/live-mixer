package session

// Track 名称约定，便于后续字幕/音频/PIP 等轨道对齐。
const (
	TrackMainVideo = "main_video"
	TrackSubtitle  = "subtitle"
	TrackTTS       = "tts"
	TrackBGM       = "bgm"
	TrackPIP       = "pip"
	TrackSticker   = "sticker"
	TrackEffect    = "effect"
)

// Timeline 草稿时间轴状态（微秒）。
type Timeline struct {
	CursorUS int64
	Tracks   map[string]*TrackState
}

// TrackState 单轨道状态。
type TrackState struct {
	Name      string
	TrackID   string
	SegmentIDs []string
}

// NewTimeline 创建空时间轴。
func NewTimeline() *Timeline {
	return &Timeline{Tracks: make(map[string]*TrackState)}
}

// Advance 将游标前移 durationUS 微秒，返回推进前的起点。
func (t *Timeline) Advance(durationUS int64) (startUS int64) {
	if t == nil {
		return 0
	}
	startUS = t.CursorUS
	t.CursorUS += durationUS
	return startUS
}

// EnsureTrack 返回命名轨道，不存在则创建。
func (t *Timeline) EnsureTrack(name string) *TrackState {
	if t == nil {
		return nil
	}
	if t.Tracks == nil {
		t.Tracks = make(map[string]*TrackState)
	}
	if tr, ok := t.Tracks[name]; ok {
		return tr
	}
	tr := &TrackState{Name: name}
	t.Tracks[name] = tr
	return tr
}
