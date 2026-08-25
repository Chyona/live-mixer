# 简创 AI 剪辑（live-mixer-ui）

面向直播 / 长视频内容的 **AI 智能切片与剪辑管理前端**。  
支持源视频入库、提示词配置、时间轴 / 人工两种切片方式、剪辑项目管理，以及一键成片、AI 选片、草稿导出等异步任务跟踪。

## 业务能力

| 模块           | 说明                                                                      |
| -------------- | ------------------------------------------------------------------------- |
| **源视频管理** | 录入直播/回放素材，查看 ASR 文案与识别状态，进入切片                      |
| **提示词管理** | 维护成片用的系统提示词，供时间轴切片选用                                  |
| **时间轴切片** | 在时间轴上框选片段，配合提示词发起一键成片 / AI 选片                      |
| **人工切片**   | 基于 ASR 文案点选/拖选片段，支持 AI 分段辅助、保存项目与提交成片          |
| **项目管理**   | 管理已保存的剪辑项目，进入编辑、查看关联任务                              |
| **任务管理**   | 查看生成草稿 / 一键成片 / AI 选片进度，复制草稿地址，下载合成视频与片段包 |
| **剪映小助手** | 外链跳转，便于将草稿导入剪映                                              |

### 典型流程

```text
源视频入库 →（可选）配置提示词
    → 时间轴切片 或 人工切片 → 保存为剪辑项目
    → 提交任务（成片 / AI 选片 / 草稿）
    → 任务管理查看进度与下载结果
```

> 项目存在**进行中 / 待进行任务**时，对应切片页会进入只读（可浏览、定位播放），避免与后台任务冲突；「AI 提示词」面板仍可编辑。

## 快速开始

```bash
pnpm install
cp .env.example .env          # 按需修改
pnpm dev                      # http://localhost:8008
```

要求：Node ≥ 20，pnpm ≥ 10。

## 常用命令

| 命令            | 说明                      |
| --------------- | ------------------------- |
| `pnpm dev`      | 开发服务（默认端口 8008） |
| `pnpm build`    | 生产构建                  |
| `pnpm preview`  | 预览构建产物              |
| `pnpm validate` | lint + 类型检查 + 单测    |

## 环境配置

| 文件              | 用途                     |
| ----------------- | ------------------------ |
| `.env.example`    | 模板，复制为 `.env`      |
| `.env`            | 本地默认（gitignore）    |
| `.env.production` | 生产打包（`pnpm build`） |

关键业务相关变量：

| 变量                          | 说明                | 示例                                     |
| ----------------------------- | ------------------- | ---------------------------------------- |
| `VITE_APP_TITLE`              | 产品名称            | 简创AI剪辑                               |
| `VITE_APP_DESCRIPTION`        | 产品描述            | 面向直播内容的 AI 智能切片与剪辑管理平台 |
| `VITE_API_PREFIX`             | 接口前缀            | `/openapi/live-mixer`                    |
| `VITE_API_PROXY_TARGET`       | 开发代理后端        | `http://127.0.0.1:30008`                 |
| `VITE_NAV_LAYOUT`             | 导航 `left` / `top` | `left`                                   |
| `VITE_LOGIN_MODE`             | `page` / `modal`    | `page`                                   |
| `VITE_JIANYING_ASSISTANT_URL` | 剪映小助手外链      | -                                        |
| `VITE_MOCK`                   | 是否启用 Mock       | `true` / `false`                         |

联调远程后端时，建议在 `.env` 中设置：

```bash
VITE_API_PROXY_TARGET=http://你的后端地址:端口
VITE_MOCK=false
```

生产环境需在网关 / Nginx 将 `{VITE_API_PREFIX}/*` 反代到 live-mixer 后端。

## 目录结构（业务相关）

```text
src/
├── pages/
│   ├── SourceVideos/        # 源视频管理
│   ├── AiPrompts/           # 提示词管理
│   ├── SourceVideoSlice/    # 时间轴切片
│   ├── ManualVideoSlice/    # 人工切片
│   ├── Slices/              # 项目管理
│   └── Tasks/               # 任务管理
├── services/                # 接口封装（源视频 / 项目 / 任务 / 提示词…）
├── components/              # 通用与切片相关组件
├── routes/                  # 菜单与路由
└── style/                   # 全局与切片页样式
mock/                        # 开发 Mock（与 VITE_API_PREFIX 对齐）
```

## 技术栈

React 18 · Vite 5 · TypeScript · Ant Design 5 · Tailwind CSS · Axios · HLS.js · Vitest

## 提示约定

业务侧用户可见提示优先使用 `toast.notify.*`（右上角通知），详见项目 Cursor 规则与 `src/utils/toast.ts`。
