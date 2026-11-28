// 文件作用：智能口播降噪 DSP 实现（FFT / 噪声谱估计 / Wiener 增益 /
// 重叠相加重建），算法与 cpp-denoise demo 保持一致，仅去掉定点压制等
// 口播场景不需要的选项，并把整段功率谱改为三次顺序扫描以控制内存。

#include "AudioDenoiser.h"

#include <algorithm>
#include <cmath>
#include <complex>
#include <vector>

namespace {

constexpr double kPi = 3.14159265358979323846;

// ---------------------------------------------------------------- FFT（radix-2）

class Fft
{
public:
    explicit Fft(int n) : n_(n)
    {
        rev_.resize(n);
        int lg = 0;
        while ((1 << lg) < n) {
            ++lg;
        }
        for (int i = 0; i < n; ++i) {
            int r = 0;
            for (int k = 0; k < lg; ++k) {
                if (i & (1 << k)) {
                    r |= 1 << (lg - 1 - k);
                }
            }
            rev_[i] = r;
        }
        tw_.resize(n / 2);
        for (int i = 0; i < n / 2; ++i) {
            tw_[i] = std::polar(1.0, -2.0 * kPi * i / n);
        }
    }

    void forward(std::vector<std::complex<double>> &a) const { transform(a, false); }

    void inverse(std::vector<std::complex<double>> &a) const
    {
        transform(a, true);
        const double inverseN = 1.0 / n_;
        for (auto &value : a) {
            value *= inverseN;
        }
    }

private:
    void transform(std::vector<std::complex<double>> &a, bool inverse) const
    {
        const int n = n_;
        for (int i = 0; i < n; ++i) {
            if (i < rev_[i]) {
                std::swap(a[i], a[rev_[i]]);
            }
        }
        for (int len = 2; len <= n; len <<= 1) {
            const int half = len / 2;
            const int step = n / len;
            for (int i = 0; i < n; i += len) {
                for (int j = 0; j < half; ++j) {
                    const std::complex<double> w =
                        inverse ? std::conj(tw_[j * step]) : tw_[j * step];
                    const std::complex<double> u = a[i + j];
                    const std::complex<double> v = a[i + j + half] * w;
                    a[i + j] = u + v;
                    a[i + j + half] = u - v;
                }
            }
        }
    }

    int n_;
    std::vector<int> rev_;
    std::vector<std::complex<double>> tw_;
};

// ---------------------------------------------------------------- 单声道降噪器

class ChannelDenoiser
{
public:
    ChannelDenoiser(const AudioDenoiser::Params &params, const float *input,
                    float *output, std::int64_t frames, int channels,
                    int channel)
        : params_(params),
          nfft_(params.nfft),
          hop_(params.hop),
          bins_(params.nfft / 2 + 1),
          fft_(params.nfft),
          window_(params.nfft),
          rawGain_(static_cast<std::size_t>(bins_), 1.0),
          smoothGain_(static_cast<std::size_t>(bins_), 1.0),
          previousGainSquared_(static_cast<std::size_t>(bins_), 0.0),
          previousGamma_(static_cast<std::size_t>(bins_), 1.0),
          noise_(static_cast<std::size_t>(bins_), 1e-12),
          input_(input),
          output_(output),
          frames_(frames),
          channels_(channels),
          channel_(channel)
    {
        for (int i = 0; i < nfft_; ++i) {
            window_[i] = 0.5 - 0.5 * std::cos(2.0 * kPi * i / nfft_); // periodic Hann
        }

        const double strength = std::clamp(params_.strength, 0.0, 1.0);
        oversubtraction_ = 0.8 + 2.6 * strength;              // 过减因子 0.8 .. 3.4
        const double floorDb = 12.0 + 16.0 * strength;        // 增益下限 12 .. 28 dB
        gainFloor_ = std::pow(10.0, -floorDb / 20.0);
    }

    void run()
    {
        const std::int64_t nfft = nfft_;
        const std::int64_t hop = hop_;
        const std::int64_t pad = nfft;
        const std::int64_t total = pad + frames_ + pad + nfft;
        frameCount_ = total >= nfft ? (total - nfft) / hop + 1 : 0;
        if (frameCount_ <= 0) {
            return;
        }

        std::vector<double> energies;
        analyzeFrames(&energies);

        // ---------- 噪声谱：取能量最低 quietFrac 的帧做逐 bin 平均。
        // 数字静音帧（能量恰为 0）不参与统计：它们没有噪声可估，占比超过
        // quietFrac 时会把噪声谱拉到 0，导致有声段几乎不被压制。
        const double quietFraction = std::clamp(params_.quietFrac, 0.0, 1.0);
        std::vector<std::int64_t> candidates;
        candidates.reserve(static_cast<std::size_t>(frameCount_));
        for (std::int64_t frame = 0; frame < frameCount_; ++frame) {
            if (energies[static_cast<std::size_t>(frame)] > 0.0) {
                candidates.push_back(frame);
            }
        }

        std::vector<double> rawNoise(static_cast<std::size_t>(bins_), 0.0);
        std::vector<std::complex<double>> spectrum(static_cast<std::size_t>(nfft));
        if (!candidates.empty()) {
            std::int64_t quietFrames =
                static_cast<std::int64_t>(candidates.size() * quietFraction);
            if (quietFrames < 1) {
                quietFrames = 1;
            }
            if (quietFrames > static_cast<std::int64_t>(candidates.size())) {
                quietFrames = static_cast<std::int64_t>(candidates.size());
            }

            std::nth_element(candidates.begin(),
                             candidates.begin() + (quietFrames - 1),
                             candidates.end(),
                             [&energies](std::int64_t a, std::int64_t b) {
                                 return energies[static_cast<std::size_t>(a)]
                                     < energies[static_cast<std::size_t>(b)];
                             });
            std::vector<unsigned char> selected(
                static_cast<std::size_t>(frameCount_), 0);
            for (std::int64_t i = 0; i < quietFrames; ++i) {
                selected[static_cast<std::size_t>(
                    candidates[static_cast<std::size_t>(i)])] = 1;
            }

            std::vector<double> accumulated(static_cast<std::size_t>(bins_), 0.0);
            for (std::int64_t frame = 0; frame < frameCount_; ++frame) {
                if (!selected[static_cast<std::size_t>(frame)]) {
                    continue;
                }
                fillFrame(frame, &spectrum);
                fft_.forward(spectrum);
                for (int bin = 0; bin < bins_; ++bin) {
                    accumulated[static_cast<std::size_t>(bin)] +=
                        std::norm(spectrum[static_cast<std::size_t>(bin)]);
                }
            }
            for (int bin = 0; bin < bins_; ++bin) {
                rawNoise[static_cast<std::size_t>(bin)] =
                    accumulated[static_cast<std::size_t>(bin)]
                    / static_cast<double>(quietFrames)
                    * 1.15; // 轻度偏置补偿
            }
        }
        // 频率 3 点平滑，避免个别 bin 估计偏低导致「漏音」。
        for (int bin = 0; bin < bins_; ++bin) {
            const int left = bin > 0 ? bin - 1 : 0;
            const int right = bin + 1 < bins_ ? bin + 1 : bins_ - 1;
            const double value =
                0.25 * rawNoise[static_cast<std::size_t>(left)]
                + 0.5 * rawNoise[static_cast<std::size_t>(bin)]
                + 0.25 * rawNoise[static_cast<std::size_t>(right)];
            noise_[static_cast<std::size_t>(bin)] = std::max(value, 1e-12);
        }

        // ---------- 加增益 + 重叠相加
        std::vector<double> accumulator(static_cast<std::size_t>(nfft), 0.0);
        std::vector<double> windowSquareSum(static_cast<std::size_t>(nfft), 0.0);
        std::int64_t nextOutput = 0;

        // 处理完第 f 帧后，[f*hop, (f+1)*hop) 的输出不再被后续帧改变。
        const auto flushUntil = [&](std::int64_t end) {
            while (nextOutput < end) {
                const std::int64_t slot = nextOutput % nfft;
                const std::int64_t index = nextOutput - pad;
                if (index >= 0 && index < frames_) {
                    const double weight = windowSquareSum[static_cast<std::size_t>(slot)];
                    const double value = weight > 1e-9
                        ? accumulator[static_cast<std::size_t>(slot)] / weight
                        : 0.0;
                    output_[index * channels_ + channel_] = static_cast<float>(value);
                }
                accumulator[static_cast<std::size_t>(slot)] = 0.0;
                windowSquareSum[static_cast<std::size_t>(slot)] = 0.0;
                ++nextOutput;
            }
        };

        for (std::int64_t frame = 0; frame < frameCount_; ++frame) {
            fillFrame(frame, &spectrum);
            fft_.forward(spectrum);
            applyGain(spectrum);
            fft_.inverse(spectrum);

            const std::int64_t position = frame * hop;
            for (int i = 0; i < nfft_; ++i) {
                const std::int64_t slot = (position + i) % nfft;
                accumulator[static_cast<std::size_t>(slot)] +=
                    spectrum[static_cast<std::size_t>(i)].real() * window_[i];
                windowSquareSum[static_cast<std::size_t>(slot)] +=
                    window_[i] * window_[i];
            }
            flushUntil((frame + 1) * hop);
        }
        flushUntil(total);
    }

private:
    // 补零后的第 paddedPosition 个样本；越界返回 0。
    double inputAt(std::int64_t paddedPosition) const
    {
        const std::int64_t index = paddedPosition - nfft_;
        if (index < 0 || index >= frames_) {
            return 0.0;
        }
        return static_cast<double>(input_[index * channels_ + channel_]);
    }

    void fillFrame(std::int64_t frame, std::vector<std::complex<double>> *spectrum)
    {
        const std::int64_t position = frame * hop_;
        for (int i = 0; i < nfft_; ++i) {
            (*spectrum)[static_cast<std::size_t>(i)] =
                std::complex<double>(inputAt(position + i) * window_[i], 0.0);
        }
    }

    // 第一遍：逐帧总能量，用于挑最安静的帧。
    void analyzeFrames(std::vector<double> *energies)
    {
        energies->assign(static_cast<std::size_t>(frameCount_), 0.0);
        std::vector<std::complex<double>> spectrum(static_cast<std::size_t>(nfft_));
        for (std::int64_t frame = 0; frame < frameCount_; ++frame) {
            fillFrame(frame, &spectrum);
            fft_.forward(spectrum);
            double energy = 0.0;
            for (int bin = 0; bin < bins_; ++bin) {
                energy += std::norm(spectrum[static_cast<std::size_t>(bin)]);
            }
            (*energies)[static_cast<std::size_t>(frame)] = energy;
        }
    }

    void applyGain(std::vector<std::complex<double>> &spectrum)
    {
        for (int bin = 0; bin < bins_; ++bin) {
            const double power = std::norm(spectrum[static_cast<std::size_t>(bin)]);
            const double gamma =
                power / (noise_[static_cast<std::size_t>(bin)] + 1e-12);
            const double posterior = std::max(gamma - oversubtraction_, 1e-6);

            // decision-directed 先验 SNR（Ephraim-Malah）
            const double prior = kAlpha * previousGainSquared_[static_cast<std::size_t>(bin)]
                                     * previousGamma_[static_cast<std::size_t>(bin)]
                                 + (1.0 - kAlpha) * std::max(posterior - 1.0, 0.0);
            double gain = prior / (1.0 + prior); // Wiener
            if (gain < gainFloor_) {
                gain = gainFloor_;
            }
            rawGain_[static_cast<std::size_t>(bin)] = gain;
            previousGamma_[static_cast<std::size_t>(bin)] = gamma;
        }

        for (int bin = 0; bin < bins_; ++bin) {
            const int left = bin > 0 ? bin - 1 : 0;
            const int right = bin + 1 < bins_ ? bin + 1 : bins_ - 1;
            const double smoothed =
                0.25 * rawGain_[static_cast<std::size_t>(left)]
                + 0.5 * rawGain_[static_cast<std::size_t>(bin)]
                + 0.25 * rawGain_[static_cast<std::size_t>(right)];

            // 快起慢落：上升沿直接跟随（保住辅音），下降沿平滑（压住音乐噪声）。
            double &current = smoothGain_[static_cast<std::size_t>(bin)];
            if (smoothed > current) {
                current = smoothed;
            } else {
                current = 0.55 * current + 0.45 * smoothed;
            }
            previousGainSquared_[static_cast<std::size_t>(bin)] = current * current;
            spectrum[static_cast<std::size_t>(bin)] *= current;
        }

        // 共轭对称，保证 ISTFT 输出为实数。
        for (int bin = 1; bin < nfft_ / 2; ++bin) {
            spectrum[static_cast<std::size_t>(nfft_ - bin)] =
                std::conj(spectrum[static_cast<std::size_t>(bin)]);
        }
        spectrum[0] = std::complex<double>(spectrum[0].real(), 0.0);
        spectrum[static_cast<std::size_t>(nfft_ / 2)] =
            std::complex<double>(spectrum[static_cast<std::size_t>(nfft_ / 2)].real(), 0.0);
    }

    static constexpr double kAlpha = 0.98;

    AudioDenoiser::Params params_;
    int nfft_;
    int hop_;
    int bins_;
    Fft fft_;
    std::vector<double> window_;
    std::vector<double> rawGain_;
    std::vector<double> smoothGain_;
    std::vector<double> previousGainSquared_;
    std::vector<double> previousGamma_;
    std::vector<double> noise_;
    double oversubtraction_ = 1.0;
    double gainFloor_ = 0.05;

    const float *input_ = nullptr;
    float *output_ = nullptr;
    std::int64_t frames_ = 0;
    std::int64_t channels_ = 1;
    int channel_ = 0;
    std::int64_t frameCount_ = 0;
};

bool isPowerOfTwo(int value)
{
    return value > 0 && (value & (value - 1)) == 0;
}

} // namespace

bool AudioDenoiser::process(const float *input, float *output,
                            std::int64_t frames, int channels,
                            const Params &params, std::string *error)
{
    if (!input || !output || frames <= 0 || channels <= 0) {
        if (error) {
            *error = "降噪输入为空。";
        }
        return false;
    }
    if (!isPowerOfTwo(params.nfft) || params.nfft < 2 || params.hop <= 0
        || params.hop >= params.nfft) {
        if (error) {
            *error = "降噪参数不合法：需要 2 的幂帧长，且 0 < hop < nfft。";
        }
        return false;
    }

    for (int channel = 0; channel < channels; ++channel) {
        ChannelDenoiser denoiser(params, input, output, frames, channels, channel);
        denoiser.run();
    }
    return true;
}
