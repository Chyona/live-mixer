# asr 包：字幕折行用的中文词表

## caption_dict.txt 是什么

`caption_dict.txt` 是成片字幕折行（超 12 字换行）用的分词词表，每行「词 频次」，只收**纯汉字、1–6 字**的词条，按词排序，当前 307,198 条 / 3.7 MB。

它不是给 ASR 用的，只服务于一件事：**换行时不要把词从中间切开**。折行前先用最大概率分词（Viterbi，见 `caption_dict.go`）把超长小句切成词，词边界即合法切点。

## 来源与许可

词表由 **jieba** 的 `dict.txt` 过滤生成。jieba 采用 MIT License：

> Copyright (c) 2013 Sun Junyi
>
> Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions:
>
> The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software.
>
> THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY, FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM, OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE SOFTWARE.

词表内嵌进二进制（`//go:embed`），不引入 Go 依赖、不读外部文件，保持「单文件可执行」的交付方式。

## 重新生成

```bash
# 取 jieba 的 dict.txt（任选其一）
pip download jieba && unzip -o jieba-*.whl 'jieba/dict.txt' -d /tmp/jieba

cd backend/internal/pkg/asr
go run gen_caption_dict.go -src /tmp/jieba/jieba/dict.txt -min-freq 3 > caption_dict.txt
```

规格：保留纯汉字、长度 1–6、频次 ≥ 3 的词条。

**为什么阈值取 3**：jieba 词表在频次 <3 的区段基本是噪声（人名、新闻里的破碎片段，如「耿京归」「位青衫」），而在 [3,9] 区段已有大量真词——尤其是本项目的领域词，如「上市公司」(3)、「新能源」(3)、「直播间」(13)。实测（3 小时直播、1899 个超长小句，用 jieba 自身分词当参考）：阈值 10 → 切点落在词内 2.9%；阈值 3 → 1.1%；阈值 1 与 3 无差别，故取 3。

## 开销

懒加载（首次折行时解析一次），实测：

| 项 | 值 |
| --- | --- |
| 内嵌体积 | 3.7 MB |
| 加载耗时 | ~84 ms（一次） |
| 常驻堆 | ~13 MB |
| 分词速度 | ~0.2 µs/字（3 小时直播的全部小句约 20 ms） |

## 与校验路径的关系

`ValidateCaptionLines`（校验 LLM 断句结果）**不**用这张词表，仍用 `caption_lexicon.go` 里人工维护的 2–3 字小词表。这是刻意的：校验一旦按大词表判定「切在词内部」，LLM 给出的整条断句会被拒、回退规则折行。所以要收紧校验，得先把非法切点**吸附**到最近合法边界（`captionWordEnds` 已提供合法边界集合），而不是直接拒绝。
