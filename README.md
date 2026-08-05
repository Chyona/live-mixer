# live-mixer

直播切片 / 直播混剪助手。基于直播音视频素材，完成语音识别、AI 智能切片与剪映草稿生成，支持一键成片，也支持 AI 选段 + 人工精修后再导出。

技术栈：**Go (Gin) + PostgreSQL**，异步 Worker 调度，对接豆包 ASR、OpenAI 兼容大模型与 capcut-mate（剪映草稿服务）。

---

## 功能介绍

### 核心能力

| 能力 | 说明 |
|------|------|
| 直播素材管理 | 录入直播回放 / 录像 URL（文件或 m3u8），自动探测分辨率与时长 |
| 语音识别（ASR） | 后台异步转写，生成分句结果、AI 分段摘要与段落结构，可下载字幕 |
| 剪辑项目 | 关联直播素材与系统提示词，维护人工粗选区间（`clips0`）与 AI/精修选段（`clips1`） |
| AI 切片 | 按项目提示词与粗选区间，调用大模型从 ASR 中筛选句段，回写 `clips1` |
| 剪映草稿 | 按 `clips1` 精确裁剪素材，调用 capcut-mate 生成剪映草稿，成功后继续 gen_video 导出成片 |
| 一键成片 | 串联「AI 切片 → 剪映草稿」，适合批量快速出片 |
| AI + 人工协同 | 可先 AI 出选段，再在项目中人工调整 `clips1`，最后单独跑草稿任务 |

### 典型工作流

```text
录入直播素材
    → ASR 转写 / 摘要
    → 创建剪辑项目（关联素材 + 提示词，可选粗选 clips0）
    → 路径 A：一键成片（AI 切片 + 草稿）
    → 路径 B：AI 切片 → 人工精修 clips1 → 生成草稿
    → 下载剪映草稿继续后期
```

### 主要 API（需 JWT，登录除外）

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/openapi/live-mixer/v1/auth/login` | 登录获取 Token |
| CRUD | `/openapi/live-mixer/v1/live-materials` | 直播素材 |
| CRUD | `/openapi/live-mixer/v1/video-projects` | 剪辑项目 |
| CRUD | `/openapi/live-mixer/v1/llm-system-prompts` | 系统提示词 |
| POST | `/openapi/live-mixer/v1/tasks/ai-slice` | 创建 AI 切片任务 |
| POST | `/openapi/live-mixer/v1/tasks/draft` | 创建剪映草稿任务 |
| POST | `/openapi/live-mixer/v1/tasks/ai-slice-draft` | 创建一键成片任务 |
| GET | `/openapi/live-mixer/v1/tasks/:id` | 轮询任务进度 / 草稿地址 |

完整接口说明见 Swagger：`http://localhost:30000/swagger/index.html`（本地调试时）。

---

## 部署方法

推荐使用 Docker Compose 一键拉起整套服务。编排文件位于 `docker/`，会启动三个容器：

| 服务 | 镜像 | 作用 | 宿主机端口 |
|------|------|------|------------|
| `live-mixer` | `gogoshine/live-mixer:latest` | 本项目 API + 后台 Worker | `30000` |
| `capcut-mate` | `gogoshine/capcut-mate-pro:latest` | 剪映草稿生成 | `31001` |
| `nginx` | `gogoshine/nginx:latest` | 静态资源与 API 反向代理 | `81` |

### 前置条件

- 已安装 Docker / Docker Compose
- 可访问的 **PostgreSQL 14+**（库需提前创建，服务本身不内置数据库）
- 对象存储（腾讯云 COS / 阿里云 OSS / 火山引擎 TOS，任选其一配置完整）
- 豆包 ASR API Key、大模型 API Key（DashScope 等 OpenAI 兼容接口）

### 部署步骤

**1. 进入部署目录并准备环境变量**

```bash
cd docker
cp .env.example .env
```

编辑 `.env`，至少填写：

| 变量 | 说明 |
|------|------|
| `APP_DATABASE_*` | PostgreSQL 主机、端口、用户、库名、密码 |
| `APP_JWT_SECRET` | JWT 签名密钥（生产务必改为随机长串） |
| `APP_STORAGE_*` | 任选 COS / OSS / TOS 一组完整配置 |
| `APP_ASR_API_KEY` | 豆包语音识别 Key |
| `APP_LLM_API_KEY` | 大模型 Key |
| `APP_CAPCUT_MATE_BASE_URL` | 剪映服务地址；Compose 内一般填 `http://nginx` |
| `APP_CAPCUT_MATE_GEN_VIDEO_BASE_URL` | 可选；`gen_video` / `gen_video_status` 共用根地址，未设置时走 `BASE_URL` |
| `APP_CAPCUT_MATE_ENABLE_GEN_VIDEO` | 是否启用 gen_video（默认 true；关闭则跳过视频生成并直接完成任务） |

非敏感项（端口、Worker 并发、日志级别等）已在 `docker-compose.yaml` 的 `environment` 中声明，可按需调整。配置优先级：**环境变量 `APP_*` > 外部 yaml > 内嵌默认配置**。

**2. 初始化数据库（首次部署必做）**

在能连上目标库的环境中执行（可用本机编译的 `envinit`，或进入容器执行）：

```bash
# 本机（需已配置好数据库连接）
go run ./cmd/envinit init

# 或在已启动的 live-mixer 容器内
docker exec -it live-mixer /app/envinit init
```

初始化会建表并写入默认账号：

| 用户名 | 默认密码 | 邮箱 |
|--------|----------|------|
| `admin` | `admin` | admin@example.com |

生产环境请立即修改密码：

```bash
go run ./cmd/envinit reset-password -p 'YourStrongPassword'
```

**3. 启动服务**

```bash
cd docker
docker compose up -d
```

首次若需本地构建镜像（而不是直接拉 Hub 镜像）：

```bash
docker compose up -d --build
```

**4. 验证**

```bash
# API 健康检查（直连 live-mixer）
curl http://localhost:30000/health

# 经 nginx 访问业务 API 前缀
curl http://localhost:81/openapi/live-mixer/v1/auth/login
```

日志挂载在 `docker/logs/`，静态与暂存目录为 `docker/html/`。

**5. 常用运维**

```bash
docker compose ps
docker compose logs -f live-mixer
docker compose down          # 停止
docker compose pull && docker compose up -d   # 更新镜像后重启
```

> **说明：** `docker-compose.yaml` 中部分 `PROXY_*` / `DRAFT_URL` 等地址可能指向内网 IP，部署到新环境时请按实际网关与域名改掉，否则草稿下载或代理会失败。

---

## 开发 / 编译 / 调试

适合二次开发与本地联调。

### 环境要求

- Go **1.25+**
- PostgreSQL **14+**
- ffmpeg（ASR 抽音频、切片裁剪依赖；Docker 镜像已内置）
- （可选）本地或远端的 capcut-mate + 对象存储 / ASR / LLM 密钥

### 项目结构

```text
live-mixer/
├── cmd/
│   ├── webserver/     # HTTP API + Worker 入口
│   ├── envinit/       # 建表 / 种子数据 / 重置密码 CLI
│   └── test/          # 临时联调脚本
├── internal/          # 业务代码（handler / service / repository / scheduler / draft …）
│   └── config/        # 配置加载；本地用 config.yaml，仓库提供 config.yaml.example
├── docker/            # Dockerfile、compose、.env.example
├── docs/              # Swagger 生成物
├── migrations/        # SQL 参考脚本
└── pkg/               # 对外可复用小工具（如统一响应）
```

### 1. 拉取依赖与本地配置

```bash
go mod download

# 本地调试配置（已被 gitignore，勿提交密钥）
cp internal/config/config.yaml.example internal/config/config.yaml
```

按本机环境编辑 `internal/config/config.yaml`：数据库、JWT、对象存储、ASR、LLM、`capcut_mate.base_url` / `capcut_mate.api_key`、`web.root_dir` 等。也可用环境变量覆盖，例如：

```bash
# Windows PowerShell
$env:APP_DATABASE_PASSWORD="your_password"
$env:APP_LLM_API_KEY="sk-xxx"
$env:APP_CAPCUT_MATE_API_KEY="your-capcut-api-key"
```

### 2. 准备数据库

```sql
CREATE DATABASE live_mixer;
CREATE USER live_mixer WITH PASSWORD 'password';
GRANT ALL PRIVILEGES ON DATABASE live_mixer TO live_mixer;
```

（用户名 / 库名需与 `config.yaml` 中一致。）

```bash
go run ./cmd/envinit init
```

| 子命令 | 说明 |
|--------|------|
| `schema` | 仅建表 |
| `seed` | 仅填充种子数据 |
| `init` | 建表 + 种子（推荐首次使用） |
| `reinit` | **清空全部表**后重建并灌默认数据 |
| `reset-password` | 重置账号密码（默认 `admin`） |

自定义配置初始化：

```bash
go run ./cmd/envinit init -config /path/to/your.yaml
# 或
APP_DATABASE_PASSWORD=xxx go run ./cmd/envinit init
```

### 3. 编译

```bash
# API 服务
go build -o webserver.exe ./cmd/webserver

# 环境初始化工具
go build -o envinit.exe ./cmd/envinit
```

Linux / macOS 去掉 `.exe` 即可。Docker 多阶段构建见 `docker/Dockerfile`（`CGO_ENABLED=0` 静态链接）。

### 4. 本地运行与调试

```bash
# 使用内嵌 / 本地 config.yaml
go run ./cmd/webserver

# 指定外部配置
go run ./cmd/webserver -config /path/to/your.yaml

# 覆盖运行模式
APP_SERVER_MODE=debug go run ./cmd/webserver
```

默认监听 `http://localhost:30000`。

**快速验证：**

```bash
curl http://localhost:30000/health

# 登录
curl -X POST http://localhost:30000/openapi/live-mixer/v1/auth/login \
  -H "Content-Type: application/json" \
  -d "{\"username\":\"admin\",\"password\":\"admin\"}"

# 带 Token 访问业务接口
curl http://localhost:30000/openapi/live-mixer/v1/live-materials \
  -H "Authorization: Bearer <token>"
```

Swagger UI：`http://localhost:30000/swagger/index.html`

日志默认写入 `logs/live-mixer.log`（单文件 10MB，最多 10 个备份）。

### 5. 测试

```bash
# 全量单测
go test ./...

# 指定包
go test ./internal/handler/v1/ -v
go test ./internal/service/ -count=1
```

### 6. 二次开发建议

- **分层：** `handler`（HTTP）→ `service`（业务）→ `repository`（数据）→ `model`；异步任务在 `internal/scheduler`，剪映流水线在 `internal/draft`。
- **新增 API：** 在对应 `handler` 增加方法并补 Swagger 注释，于 `internal/routes/v1` 注册路由；需鉴权的放在 `JWTAuth` 分组下。
- **配置：** 新配置项同时更新 `config.yaml.example`、`config` 结构体与 `docker/.env.example` / compose，保持 `APP_*` 环境变量可覆盖。
- **任务类型：** AI 切片、草稿、一键成片均为异步任务，创建后返回 task，客户端轮询 `GET /tasks/:id`。
- **本地无 capcut-mate：** 可只起 `docker compose up -d capcut-mate nginx`，本机 `go run` 的 `capcut_mate.base_url` 指向 `http://localhost:81`（或实际代理地址）。

### 7. 发布镜像（维护者）

推送形如 `v1.0.0` 的 tag 后，GitHub Actions（`.github/workflows/ci.yml`）会构建并推送 Docker Hub 镜像 `*/live-mixer`。
