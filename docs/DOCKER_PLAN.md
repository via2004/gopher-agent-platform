# GopherAI Docker 学习与实施计划

## 目标

将当前可以在开发机运行的 GopherAI backend，整理为别人 clone 仓库后可以通过 Docker 构建和运行的小型可部署系统。

本轮只做 Docker 和 Docker Compose，不引入 Kubernetes、微服务拆分、服务网格或复杂部署平台。

最终希望做到：

```bash
docker compose up --build
```

能够启动：

```text
Backend
PostgreSQL
Valkey
RabbitMQ
Migration
```

并通过现有 Chat、SSE、ChatJob、ONNX 图片识别和 RAG 验收。

## 工作方式

- 同一时间只学习和实现一个阶段。
- 每个阶段开始前先解释概念和文件写法。
- 每个阶段完成后进行 review 和实际验收。
- 不把真实 `.env`、API Key 或数据库密码构建进镜像。
- 不为了 Docker 大规模修改现有业务架构。
- 每个阶段单独提交，方便回退和复盘。

## 基础概念

### Image

Image（镜像）是只读运行包，包含程序以及运行程序所需的文件。

Backend 镜像最终需要包含：

```text
Go server 可执行文件
系统运行库
CA 证书
ONNX Runtime 动态库
MobileNetV2 ONNX 模型
ImageNet 标签文件
```

### Container

Container（容器）是镜像启动后的进程实例。

```text
Image
  + 环境变量
  + 网络
  + Volume
  -> Container
```

停止容器只是停止进程；删除容器会删除其可写层中的临时文件。

### Volume

Volume（数据卷）用于保存不应随容器删除的数据。

本项目需要持久化：

```text
PostgreSQL 数据
Valkey 数据
RabbitMQ 数据
RAG 原始文档
```

Go 二进制和 ONNX 模型属于镜像内容，不属于运行数据。

### Network

Compose 会为服务创建内部网络，容器通过服务名互相访问。

Backend 容器中的 `localhost` 只表示 Backend 容器自身，不能表示 PostgreSQL 或 Valkey。

容器内连接地址应类似：

```env
DATABASE_URL=postgres://postgres:password@postgres:5432/gopherai
REDIS_URL=redis://valkey:6379/0
RABBITMQ_URL=amqp://user:password@rabbitmq:5672/gopherai
```

其中 `postgres`、`valkey`、`rabbitmq` 是 Compose service 名称。

### Dockerfile

Dockerfile 描述如何构建一个镜像。

本项目的 Dockerfile 只负责 Backend 镜像，不负责启动 PostgreSQL、Valkey 或 RabbitMQ。

### Docker Compose

Compose 描述多个容器如何一起运行，包括：

```text
使用哪些镜像
环境变量
端口映射
服务依赖
健康检查
Volume
内部网络
```

## 当前项目的特殊约束

### HTTP 监听地址

当前服务固定监听：

```text
127.0.0.1:8080
```

容器外无法访问容器内部的 `127.0.0.1`，因此需要增加：

```env
HTTP_ADDR=0.0.0.0:8080
```

本地默认值仍可保持：

```text
127.0.0.1:8080
```

### CGo 与 ONNX Runtime

`onnxruntime_go` 使用 CGo，官方 ONNX Runtime Linux 包依赖 glibc。

因此第一版镜像选择 Debian，不使用 Alpine，也不使用 `scratch`：

```text
编译阶段：golang:1.25-bookworm
运行阶段：debian:bookworm-slim
```

构建时需要：

```env
CGO_ENABLED=1
GOOS=linux
GOARCH=amd64
```

### 外部 HTTPS

Backend 会调用 Chat 和 Embedding Provider，因此运行镜像需要 CA 证书：

```text
ca-certificates
```

### 模型资产

当前 Git 忽略：

```text
models/*.onnx
models/onnxruntime/
```

别人 clone 后不会拥有这些文件。

最终 Docker 构建应下载固定版本资产并校验 SHA-256：

```text
ONNX Runtime 1.22.0
mobilenetv2-7.onnx
```

`models/imagenet_classes.txt` 已提交到 Git，可以直接复制。

### RAG 文档

RAG 文档是用户运行时数据，需要挂载 Volume：

```text
/app/data/rag
```

环境变量：

```env
RAG_STORAGE_ROOT=/app/data/rag
```

## 阶段 1：容器运行配置

状态：`completed`

完成记录：

- 新增 `HTTP_ADDR` 配置。
- 未设置时默认监听 `127.0.0.1:8080`。
- 容器可覆盖为 `0.0.0.0:8080`。
- Server 地址和启动日志使用同一个配置值。
- 已覆盖默认值、覆盖值和空格清理测试。

任务：

- 增加 `HTTP_ADDR` 环境变量。
- 本地默认监听 `127.0.0.1:8080`。
- 容器配置监听 `0.0.0.0:8080`。
- 补配置测试。

验收条件：

```text
未设置 HTTP_ADDR 时保持当前行为；
设置 HTTP_ADDR 时 server 使用指定地址；
go test ./... 和 go vet ./... 通过。
```

## 阶段 2：`.dockerignore`

状态：`completed`

完成记录：

- 排除 Git/IDE 元数据。
- 排除真实 `.env`，但显式保留 `.env.example`。
- 排除 `data`、`tmp`、日志和本地构建产物。
- 排除本地 ONNX 模型和 Runtime，后续由 Dockerfile 下载固定版本。
- 保留 Go 源码、migration 和 `models/imagenet_classes.txt`。

目的：控制哪些文件会被发送给 Docker build context。

至少忽略：

```text
.git
.env
tmp
data
*.log
本地构建产物
```

注意：如果 Dockerfile 在构建阶段下载模型，则本地 `models/*.onnx` 和 `models/onnxruntime` 也应忽略。

验收条件：

```text
真实 .env 不进入 build context；
Git 历史和运行数据不进入镜像；
Dockerfile 需要的源码和标签文件仍可复制。
```

## 阶段 3：Backend Dockerfile

状态：`pending`

使用多阶段构建。

### Builder 阶段

职责：

```text
下载 Go modules
编译 Linux amd64 server
下载并校验 ONNX Runtime
下载并校验 MobileNetV2 模型
```

大致结构：

```dockerfile
FROM golang:1.25-bookworm AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=1 GOOS=linux GOARCH=amd64 \
    go build -trimpath -o /out/server ./cmd/server
```

### Runtime 阶段

职责：

```text
安装 CA 证书
创建非 root 用户
复制 server
复制 ONNX Runtime、模型和标签
创建 RAG 数据目录
启动 server
```

大致结构：

```dockerfile
FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /out/server /app/server

EXPOSE 8080

ENTRYPOINT ["/app/server"]
```

不要把 `.env` 写入 Dockerfile，也不要使用 `ENV` 写入真实密钥。

验收条件：

```text
docker build 成功；
镜像中不存在 Go 源码和真实 .env；
server 能加载 ONNX Runtime 与模型；
容器能访问外部 HTTPS。
```

## 阶段 4：单独运行 Backend 容器

状态：`pending`

暂时复用宿主机基础设施，验证 Backend 镜像本身。

需要理解宿主机地址与容器地址的区别。Linux 环境下可使用 host network 做第一轮验证，之后 Compose 改为内部 service DNS。

验收内容：

```text
/healthz
/readyz
注册登录
普通 Chat
ONNX 图片识别
RAG 上传和问答
优雅退出
```

## 阶段 5：Compose 基础设施

状态：`pending`

新增：

```text
compose.yaml
```

服务：

```text
postgres
valkey
rabbitmq
backend
```

需要为基础设施配置 healthcheck，并让 Backend 使用 service DNS 连接。

端口只暴露开发和验收需要的部分：

```text
backend:   8080
postgres:  5432（可选，仅本地调试）
valkey:    6379（可选，仅本地调试）
rabbitmq:  5672、15672（管理页面可选）
```

验收条件：

```text
docker compose up 可以启动全部基础设施；
Backend 不使用 localhost 连接其他容器；
服务健康检查通过。
```

## 阶段 6：Migration 自动化

状态：`pending`

目标：新环境不再手动逐个执行 SQL 文件。

建议使用一次性 migration service：

```text
PostgreSQL ready
  -> migration 执行所有 up migrations
  -> backend 启动
```

需要保证：

```text
Migration 失败时 Backend 不启动；
重复启动不会重复破坏 schema；
现有数据库升级路径明确。
```

具体工具在进入本阶段前再决定，不提前引入。

## 阶段 7：Volume 与重启恢复

状态：`pending`

为以下数据增加 Volume：

```text
PostgreSQL
Valkey
RabbitMQ
RAG 文档
```

验证：

```text
上传文档并创建会话；
docker compose down；
docker compose up；
用户、会话、ChatJob、Redis current version 和 RAG 文件仍存在。
```

注意：

```bash
docker compose down -v
```

会删除 Compose Volume，不能用于持久化验收。

## 阶段 8：最终验收与文档

状态：`pending`

任务：

- 运行 `go test ./...`。
- 运行 `go vet ./...`。
- 执行完整 Compose E2E。
- 记录构建、启动、停止、日志、重建和数据清理命令。
- 在 backend 自己的 README 中增加 Docker 使用说明。
- 确认 `.env` 和密钥未被提交或打入镜像。

## 暂不处理

- Kubernetes
- Helm
- 多环境配置中心
- 自动扩缩容
- 服务网格
- 镜像仓库 CI/CD
- GPU ONNX Runtime
- 跨架构镜像（第一版只支持 linux/amd64）

## 常用命令预览

这些命令会在对应阶段逐个学习和执行：

```bash
docker build -t gopherai-backend:dev .
docker image ls
docker run --rm ... gopherai-backend:dev
docker compose up --build
docker compose ps
docker compose logs -f backend
docker compose down
```
