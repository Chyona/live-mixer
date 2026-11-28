#pragma once

// 文件作用：智能口播的人声音轨降噪（纯 C++，零第三方 DSP 依赖）。
// 算法移植自 cpp-denoise demo：
//   STFT(1024/256, periodic Hann)
//   → 按帧能量挑最安静的一批帧估计噪声功率谱（跳过数字静音帧）
//   → 逐帧 decision-directed 先验 SNR + Wiener 增益 + 快起慢落平滑
//   → ISTFT 重叠相加（窗平方和归一化）。
// 噪声谱估计拆成两次顺序扫描，不需要为整段音频保存功率谱；因此长视频
// 也只占用每帧 O(nfft) 的内存。调用方负责用 ffmpeg 解码 / 封装。

#include <cstdint>
#include <string>

namespace AudioDenoiser {

struct Params
{
    // 0..1：越大越干净、人声越干。智能口播三档的映射见
    // OralPipelineService::denoiseVoice。
    double strength = 0.5;
    // 帧长与帧移；默认 75% 重叠。
    int nfft = 1024;
    int hop = 256;
    // 用于估计噪声谱的「最安静帧」比例。
    double quietFrac = 0.18;
};

// 对交错 f32 PCM 逐声道降噪。input / output 均指向 frames*channels 个
// float；frames 是每个声道的样本数，channels 是声道数。
// 允许原地处理（input == output）。
bool process(const float *input, float *output, std::int64_t frames,
             int channels, const Params &params, std::string *error);

} // namespace AudioDenoiser
