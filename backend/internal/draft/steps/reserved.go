package steps

// 预留步骤名：实现对应 Step 并加入 Recipe 后启用。
// captions 已接入 DefaultRecipe（add_videos 之后）；其余勿加入 DefaultRecipe。
const (
	NameCaptions   = "captions"    // 字幕（add_captions，来源 ASR）
	NameTTS        = "tts"         // 字幕音 / TTS
	NameBGM        = "bgm"         // 背景音乐
	NameEffects    = "effects"     // 视频特效
	NameFilters    = "filters"     // 滤镜
	NamePIP        = "pip"         // 画中画
	NameStickers   = "stickers"    // 贴纸
	NameFlowerText = "flower_text" // 花字
)
