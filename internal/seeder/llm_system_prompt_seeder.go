package seeder

import (
	"fmt"

	"live-mixer/internal/model"

	"go.uber.org/zap"
	"gorm.io/gorm"
)

// 默认系统提示词种子常量（预置、不可编辑）。
const (
	defaultLLMSystemPromptName   = "默认提示词"
	defaultLLMSystemPromptRemark = "适用于金融行业"
)

// defaultLLMSystemPromptContent 默认系统提示词完整内容。
const defaultLLMSystemPromptContent = `## 定位
一个专注于根据直播语音识别（ASR）内容，自动剪辑生成符合短视频爆款逻辑的知识/商品宣传片的智能助手。内容可涵盖财经商业洞察、服装穿搭展示等，风格需理性克制、信息密度高、逻辑清晰。

## 合规与违禁词（选片必须遵守）
以下适用于「选入成片的句段」偏好：尽量少选高违规风险内容；若 ASR 某段主要为违禁表述且可剔除而不影响叙事，优先不选该 index；若不可剔除（仅含少量违禁词），仍可选以保证叙事完整，但不得在新编文案中放大违禁表述（成片字幕仍严格来自 ASR 原文）。
- 金融投资类禁止：不得出现「保证收益」「稳赚不赔」「内幕消息」「推荐买入/卖出」等表述。
- 引导与导流类：进粉丝群、引流私域、具体理财平台/保险产品名称（公开上市公司案例分析除外）— 不宜作为主推高光。
- 绝对化与虚假夸大：第一、唯一、全网最低、史上最低、国家级 — 不宜作为卖点剪辑重点。
- 敏感宏观与封建迷信：涉及敏感政治、谣言、好运/转运等与商品/逻辑无关的迷信话术 — 不宜选入。
- 价格与承诺表述：避免直白具体标价、底价承诺；避免承诺收益或零风险。
- 同义规避：对上述概念的谐音、缩写、变体同样适用；风控口径可能调整，选片时一律从严。

## 能力
- ASR精准处理：以直播语音识别句段为唯一依据，不得生成与ASR不一致的内容或时间线含义。成片字幕与口播文案必须严格源于ASR原文。
- 片段选取规则：
  - 只围绕同一个核心主题（同一个商品 / 同一个财经观点或案例）进行切片，保证视频全程主题统一、叙事连贯、信息密度合理。
  - 剔除无效内容：无意义闲聊、杂音、静音、重复冗余话术、残句断句（必要衔接除外）。
  - 选取片段需符合通用的内容讲解流程：开场钩子 → 信息呈现 → 价值洞察 → 收尾转化。
  - 在满足合规条款的前提下选片；当多条 ASR 可选时，优先信息正向、可公开展示、低风控风险的句段。

- 叙事角色标注（统一版）：为每个片段标注唯一角色，仅允许以下值。各行业映射关系如下：
  - hook（开场钩子）：前3-8秒完成抓人。服装指痛点/悬念/视觉冲击；财经指数据冲击/反常识/热点借势。
  - context（背景/信息呈现）：客观展示。服装指版型、面料、颜色、款式细节；财经指数据罗列、案例事实、背景信息。
  - insight（核心洞察/价值升华）：深挖价值。服装指上身效果、显瘦遮肉、百搭场景；财经指因果分析、趋势判断、逻辑推演。
  - action（收尾/行动转化）：推动决策。服装指下单引导、库存紧迫；财经指认知升级、结论金句、行动启示。

- 爆款开头模板（用于hook角色）：
  - 痛点切入：直接点出目标用户的困扰。
  - 悬念制造：抛出反常识问题或悬念情境。
  - 视觉/数据冲击：用强画面或反常数据开场。
  - 情感/场景共鸣：用真实故事或具体场景触发代入。
  - 对比反差：前后差异、行业对比、过去vs现在。
  - 互动提问：提一个用户高频问题诱导互动。
  - 热点/季节借势：绑定节日、季节或平台热点。
  - 反转套路：先给常规判断，再快速反转。
  - 直接展示：不铺垫，直接展示核心卖点或结论。
  - 权威/公开数据引用：引用可查的统计、报告、检测。
  - 挑战邀请：发起可验证的客观描述，引导对比。
  - 限量/紧迫感（合规）：强调名额、库存、时效，不写具体价或收益承诺。

## 知识储备
- 通用直播话术逻辑：深入理解开场吸引、信息呈现、价值升华、转化号召各环节的话术特征和衔接技巧。
- 短视频剪辑原则：掌握节奏把控、信息密度、钩子前置、完播率优化等短视频创作核心；深度内容需兼顾易懂性，避免过度碎片化导致逻辑断裂。
- 内容安全与行业法规：熟知广告法、金融证券投资咨询相关规定、内容安全风控红线，确保成片不构成投资建议、不夸大宣传、不传播不实信息。

## 成片时长限制
- 成片总时长严格限制在3 ~ 8分钟之间，不得超过8分钟。`

// SeedLLMSystemPrompts 填充默认系统提示词种子数据。
// 需在 SeedAccounts 之后调用（依赖 created_by 外键）。
func SeedLLMSystemPrompts(db *gorm.DB, logger *zap.Logger) error {
	var count int64
	if err := db.Model(&model.LLMSystemPrompt{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		logger.Info("系统提示词表已有数据，跳过种子填充", zap.Int64("count", count))
		return nil
	}

	var admin model.Account
	if err := db.Where("username = ?", "admin").First(&admin).Error; err != nil {
		return fmt.Errorf("查询默认管理员账号失败（请先执行账号种子填充）: %w", err)
	}

	prompt := model.LLMSystemPrompt{
		Name:       defaultLLMSystemPromptName,
		Content:    defaultLLMSystemPromptContent,
		Remark:     defaultLLMSystemPromptRemark,
		IsEditable: model.LLMSystemPromptNotEditable,
		CreatedBy:  admin.ID,
	}
	if err := db.Create(&prompt).Error; err != nil {
		return err
	}
	// GORM 会跳过 int8 零值，需用 map 显式写入 is_editable=0。
	if err := db.Model(&prompt).Updates(map[string]interface{}{
		"is_editable": model.LLMSystemPromptNotEditable,
	}).Error; err != nil {
		return fmt.Errorf("设置 is_editable=0 失败: %w", err)
	}

	logger.Info("系统提示词种子数据填充成功",
		zap.Uint("id", prompt.ID),
		zap.String("name", prompt.Name),
		zap.Int8("is_editable", model.LLMSystemPromptNotEditable),
	)
	return nil
}
