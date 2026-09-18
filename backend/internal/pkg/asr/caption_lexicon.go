package asr

import (
	"sync"
	"unicode"
	"unicode/utf8"
)

// captionLexiconMaxRune 正向最大匹配尝试的最长词长（字）。
const captionLexiconMaxRune = 4

// captionLexiconWords 成片字幕折行用轻量中文词表（财经/商业 + 媒体/直播/通用）。
// 手工维护；命中词作为 keepIntact 原子，避免「金融」「银行」被拦腰切开。
var captionLexiconWords = []string{
	// —— 财经 / 商业（约 200）——
	"金融", "银行", "市场", "经济", "企业", "投资", "风险", "利率", "股票", "基金",
	"债券", "证券", "期货", "期权", "汇率", "货币", "资本", "资产", "负债", "利润",
	"营收", "收入", "成本", "费用", "现金流", "估值", "市值", "股价", "涨跌", "涨停",
	"跌停", "牛市", "熊市", "泡沫", "危机", "衰退", "复苏", "通胀", "通缩", "紧缩",
	"宽松", "政策", "央行", "美联储", "财政", "税收", "补贴", "关税", "贸易", "出口",
	"进口", "供应链", "产业链", "制造业", "服务业", "房地产", "楼市", "房价", "地产", "物业",
	"融资", "上市", "IPO", "并购", "重组", "破产", "清算", "违约", "信用", "杠杆",
	"流动性", "准备金", "存款准备金", "存款", "贷款", "信贷", "按揭", "抵押", "担保", "保险",
	"理财", "储蓄", "支付", "结算", "清算", "账户", "余额", "收益", "回报", "亏损",
	"盈利", "分红", "分红率", "市盈率", "净资产", "股东", "股权", "股份", "控股", "参股",
	"董事会", "管理层", "创业", "创业板", "科创板", "主板", "港股", "美股", "A股", "指数",
	"沪深", "纳斯达克", "道琼斯", "标普", "黄金", "原油", "大宗", "商品", "能源", "新能源",
	"光伏", "锂电", "芯片", "半导体", "科技", "互联网", "电商", "零售", "消费", "品牌",
	"营销", "广告", "渠道", "供应链", "物流", "仓储", "库存", "订单", "客户", "用户",
	"流量", "转化", "复购", "客单价", "毛利", "净利", "同比", "环比", "增速", "增长",
	"下滑", "回暖", "企稳", "突破", "支撑", "压力", "趋势", "周期", "景气", "预期",
	"预测", "分析", "研报", "机构", "券商", "基金经理", "投资者", "散户", "主力", "游资",
	"监管", "合规", "审计", "披露", "财报", "年报", "季报", "业绩", "指引", "展望",
	"战略", "布局", "赛道", "风口", "红利", "壁垒", "护城河", "竞争", "垄断", "寡头",
	"全球化", "本土化", "数字化", "智能化", "自动化", "转型升级", "降本增效", "高质量发展",
	"共同富裕", "双循环", "内需", "外需", "消费升级", "降息", "加息", "降准", "升息", "基准利率",
	"国债", "地方债", "城投", "专项债", "赤字", "预算", "债务", "坏账", "不良", "拨备",
	"拨备覆盖率", "资本充足率", "系统重要性", "影子银行", "互联网金融", "普惠金融", "绿色金融",
	"数字货币", "比特币", "区块链", "元宇宙", "人工智能", "大模型", "算法", "数据", "算力",
	"云计算", "物联网", "新能源车", "电动车", "自动驾驶", "碳中和", "碳达峰", "ESG",
	"可持续发展", "社会责任", "公司治理", "透明度", "信息披露", "内幕交易", "操纵市场",
	"注册制", "退市", "停牌", "复牌", "解禁", "减持", "增持", "回购", "分红", "送转",
	"配股", "定增", "可转债", "优先股", "普通股", "流通股", "总股本", "市净率", "市销率",
	"净资产收益率", "毛利率", "净利率", "资产负债率", "速动比率", "流动比率", "周转率",
	"应收账款", "应付账款", "固定资产", "无形资产", "商誉", "减值", "计提", "核销",
	"对冲", "套保", "套利", "做多", "做空", "多头", "空头", "仓位", "建仓", "平仓",
	"止损", "止盈", "加仓", "减仓", "满仓", "空仓", "换手", "成交额", "成交量", "换手率",
	"市盈率", "市净率", "股息率", "分红率", "市值", "流通市值", "总市值", "估值中枢",

	// —— 媒体 / 直播 / 通用（约 100）——
	"直播", "频道", "内容", "用户", "平台", "视频", "音频", "字幕", "剪辑", "成片",
	"切片", "素材", "项目", "任务", "草稿", "时间轴", "进度", "解析", "转写", "识别",
	"观众", "粉丝", "主播", "嘉宾", "主持", "评论", "弹幕", "点赞", "转发", "分享",
	"关注", "订阅", "会员", "付费", "免费", "开播", "关播", "回放", "录播", "连麦",
	"互动", "提问", "回答", "讲解", "分享", "推荐", "精选", "热门", "爆款", "流量",
	"曝光", "转化", "留存", "活跃", "日活", "月活", "时长", "完播", "完播率", "点击率",
	"封面", "标题", "简介", "标签", "话题", "热点", "资讯", "新闻", "报道", "访谈",
	"对话", "讨论", "观点", "看法", "建议", "提醒", "注意", "重要", "关键", "核心",
	"重点", "亮点", "痛点", "难点", "机会", "挑战", "问题", "方案", "方法", "路径",
	"步骤", "流程", "环节", "阶段", "过程", "结果", "效果", "影响", "意义", "价值",
	"今天", "明天", "昨天", "今年", "明年", "去年", "目前", "现在", "未来", "过去",
	"我们", "你们", "他们", "大家", "朋友", "听众", "读者", "客户", "企业", "公司",
	"中国", "美国", "全球", "国内", "海外", "本地", "线上", "线下", "互联网", "新媒体",
	"短视频", "长视频", "图文", "专栏", "系列", "节目", "栏目", "专题", "活动", "峰会",
	"论坛", "会议", "发布会", "路演", "招商", "合作", "伙伴", "生态", "社区", "品牌",
}

var (
	captionLexiconOnce sync.Once
	captionLexiconSet  map[string]struct{}
	captionLexiconMax  int
)

func ensureCaptionLexicon() {
	captionLexiconOnce.Do(func() {
		captionLexiconSet = make(map[string]struct{}, len(captionLexiconWords))
		maxLen := 0
		for _, w := range captionLexiconWords {
			w = normalizeLexiconWord(w)
			if w == "" {
				continue
			}
			n := utf8.RuneCountInString(w)
			if n < 2 || n > captionLexiconMaxRune {
				continue
			}
			captionLexiconSet[w] = struct{}{}
			if n > maxLen {
				maxLen = n
			}
		}
		if maxLen == 0 {
			maxLen = 2
		}
		captionLexiconMax = maxLen
	})
}

func normalizeLexiconWord(w string) string {
	runes := make([]rune, 0, utf8.RuneCountInString(w))
	for _, r := range w {
		if unicode.IsSpace(r) {
			continue
		}
		runes = append(runes, r)
	}
	return string(runes)
}

// buildExtraCJKWords 从 ASR Words 提取长度 ≥2 的中文词，供当次折行动态合并。
func buildExtraCJKWords(words []Word) map[string]struct{} {
	if len(words) == 0 {
		return nil
	}
	out := make(map[string]struct{})
	for _, w := range words {
		s := normalizeLexiconWord(w.Text)
		n := utf8.RuneCountInString(s)
		// 动态词允许到单行上限；超长词仍整段 keepIntact（由折行允许 > max）。
		if n < 2 {
			continue
		}
		allHan := true
		for _, r := range s {
			if !unicode.Is(unicode.Han, r) {
				allHan = false
				break
			}
		}
		if allHan {
			out[s] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// matchCJKWord 从 i 起正向最大匹配中文词（静态词表 ∪ extra）；未命中返回 i。
func matchCJKWord(runes []rune, i int, extra map[string]struct{}) int {
	if i >= len(runes) || !unicode.Is(unicode.Han, runes[i]) {
		return i
	}
	ensureCaptionLexicon()
	maxTry := captionLexiconMax
	if extra != nil {
		for w := range extra {
			if n := utf8.RuneCountInString(w); n > maxTry {
				maxTry = n
			}
		}
	}
	remain := len(runes) - i
	if remain < maxTry {
		maxTry = remain
	}
	for L := maxTry; L >= 2; L-- {
		cand := string(runes[i : i+L])
		if L <= captionLexiconMaxRune {
			if _, ok := captionLexiconSet[cand]; ok {
				return i + L
			}
		}
		if extra != nil {
			if _, ok := extra[cand]; ok {
				return i + L
			}
		}
	}
	return i
}

// CaptionLexiconSize 返回内置词表有效词数（测试用）。
func CaptionLexiconSize() int {
	ensureCaptionLexicon()
	return len(captionLexiconSet)
}
