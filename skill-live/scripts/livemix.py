#!/usr/bin/env python3
"""开源直播切片助手 CLI。

用法：
    python3 scripts/livemix.py doctor
    python3 scripts/livemix.py record https://live.douyin.com/123456
    python3 scripts/livemix.py clip --input demo.mp4 --prompt 默认提示词
    python3 scripts/livemix.py download-draft --draft-url '...'
    python3 scripts/livemix.py export --draft-url '...'

所有命令把进度写到 stderr，把结果 JSON 写到 stdout，方便脚本与 Agent 集成。
"""

from __future__ import annotations

import os
import sys

sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))

from cli import main  # noqa: E402

if __name__ == "__main__":
    raise SystemExit(main())
