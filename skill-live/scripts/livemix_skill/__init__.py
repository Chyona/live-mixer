"""LiveMix Studio 开源直播切片助手（skill 实现）。

本包把 LiveMix Studio 客户端的「直播录制 → 本地素材 → 一键成片 →
剪映草稿 / 成片导出」能力完整移植为可脚本化、可被 Agent 调用的 Python 工具。

设计原则：
- 与客户端逐参数对齐：ffmpeg 参数、剪映小助手请求字段、ASR / LLM 请求
  与解析、字幕断句与时间对齐、成片包装随机池、草稿下载本地化流程，
  均以 LiveMix/src 下的 C++ 实现为唯一基准。
- 仅使用 Python 标准库，不引入第三方依赖。
"""

__version__ = "1.0.0"
__skill_name__ = "livemix-live-clipper"
__display_name__ = "开源直播切片助手"
