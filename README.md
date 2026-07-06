# Base

基于 **Gin + PostgreSQL** 的基础 API 服务，提供账号管理 RESTful 接口。

## 环境要求

- Go 1.25+
- PostgreSQL 14+

## 项目结构

```
base/
├── cmd/
│   ├── webserver/          # HTTP API 服务入口
│   └── envinit/            # 环境初始化 CLI（建表、种子数据）
├── internal/               # 业务代码（含内嵌 config.yaml）
├── docker/                 # 容器部署（Dockerfile、compose、.env）
├── docs/                   # Swagger 文档（docs.go）
└── logs/                   # 运行时日志（自动创建）
```

## 配置说明

配置加载优先级（从高到低）：

1. 环境变量 `APP_*`（容器部署时使用）
2. 外部配置文件（`-config` 参数指定）
3. 内嵌默认配置 `internal/config/config.yaml`（本地调试默认使用）

本地调试可直接使用内嵌配置，默认连接：

| 配置项 | 默认值 |
|--------|--------|
| 服务端口 | `30000` |
| 数据库地址 | `127.0.0.1:5432` |
| 数据库名 | `base_db` |
| 数据库用户 | `postgres` / `postgres` |

## 初始化

### 1. 安装依赖

```bash
go mod download
```

### 2. 准备 PostgreSQL 数据库

确保 PostgreSQL 已启动，并创建数据库（若尚未创建）：

```sql
CREATE DATABASE base_db;
```

### 3. 执行环境初始化

`envinit` 提供三个子命令：

| 命令 | 说明 |
|------|------|
| `schema` | 仅初始化数据库表结构（GORM AutoMigrate） |
| `seed` | 仅填充基础数据 |
| `init` | 一键执行：建表 + 填充基础数据 |

**推荐：首次使用执行一键初始化**

```bash
go run ./cmd/envinit init
```

分步执行：

```bash
go run ./cmd/envinit schema   # 建表
go run ./cmd/envinit seed     # 填充种子数据
```

初始化完成后会创建默认账号（密码均为 `123456`）：

| 用户名 | 邮箱 |
|--------|------|
| admin | admin@example.com |
| demo | demo@example.com |

### 4. 使用自定义配置初始化

若本地数据库配置与默认不同，可通过环境变量或外部配置文件覆盖：

```bash
# 环境变量
APP_DATABASE_PASSWORD=your_password go run ./cmd/envinit init

# 外部配置文件（任意路径的 yaml）
go run ./cmd/envinit init -config /path/to/your.yaml
```

## 本地调试

### 启动 API 服务

```bash
go run ./cmd/webserver
```

服务默认监听 `http://localhost:30000`。

### 使用自定义配置启动

```bash
go run ./cmd/webserver -config /path/to/your.yaml
```

或通过环境变量覆盖，例如：

```bash
APP_SERVER_MODE=debug go run ./cmd/webserver
```

### 验证服务

```bash
# 健康检查
curl http://localhost:30000/health

# 查询账号列表
curl http://localhost:30000/openapi/live-mixer/v1/accounts
```

### Swagger 文档

浏览器访问：

```
http://localhost:30000/swagger/index.html
```

### 鉴权说明

部分写操作接口需要 Bearer Token，示例：

```bash
curl -X PUT http://localhost:30000/openapi/live-mixer/v1/accounts/1 \
  -H "Authorization: Bearer demo-token" \
  -H "Content-Type: application/json" \
  -d '{"nickname":"新昵称"}'
```

### 日志

日志输出到 `logs/base.log`，单文件最大 10MB，最多保留 10 个历史文件。

```bash
tail -f logs/base.log
```

## 容器部署（可选）

```bash
cd docker
cp .env.example .env   # 编辑 .env 填入数据库信息
docker compose up -d --build
```

详细配置说明见 `docker/docker-compose.yaml` 与 `docker/.env.example`。

## 常用 API 路径

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/openapi/live-mixer/v1/accounts` | 账号列表 |
| POST | `/openapi/live-mixer/v1/accounts` | 创建账号 |
| GET | `/openapi/live-mixer/v1/accounts/:id` | 账号详情 |
| PUT | `/openapi/live-mixer/v1/accounts/:id` | 更新账号（需鉴权） |
| DELETE | `/openapi/live-mixer/v1/accounts/:id` | 删除账号（需鉴权） |
| GET | `/openapi/live-mixer/v2/accounts/:id/profile` | 账号概要（v2） |
