# voex-server

Gin + GORM HTTP 服务：账号认证、团队与项目权限、单层目录、Markdown 文档、附件、资产图片与文件分享。默认监听 `127.0.0.1:8090`，API 前缀 `/api`。

## 环境与运行

- Go 1.27.1。服务器安装于 `/opt/go1.27.1`，`go` 和 `gofmt` 位于 `/usr/local/bin`。
- Make 与 libvips 命令行工具：Ubuntu `apt-get install --no-install-recommends make libvips-tools`。
- MySQL 8，完整表结构见 `internal/database/schema.sql`，连接配置见 `.env.example`。

```sh
cd /home/voex-server
# 仅新部署需要复制示例；已有 .env 时保留现有配置。
cp -n .env.example .env
chmod 600 .env
make build
make check
./bin/voex-server
```

`.env` 自动加载，进程环境变量优先；`VOEX_ENV_FILE` 可指定其他配置文件。依赖固定在 `go.mod` 和 `go.sum`。默认模块代理不可达时使用：

```sh
GOPROXY=https://goproxy.cn,direct go mod download
```

保持 Go checksum 校验。`make build` 通过临时文件构建并原子替换 `bin/voex-server`；`make fmt` 格式化 `cmd` 和 `internal`；`make check` 执行 `go vet` 和 `go mod verify`。

## 项目结构

项目采用 Gin + GORM 分层实现，入口为 `cmd/voex-server/main.go`。

| 目录 | 职责 |
| --- | --- |
| `internal/app`、`internal/config` | 依赖组装、服务生命周期和环境配置 |
| `internal/router`、`internal/middleware` | Gin 路由组、登录、CORS、日志、错误恢复 |
| `internal/handler`、`internal/transport` | 每条路由的简短中文注释、参数解析与响应 |
| `internal/service` | 业务规则、权限、事务、版本冲突 |
| `internal/repository`、`internal/model` | GORM 查询与数据模型 |
| `internal/database` | MySQL 连接、嵌入 schema、受控初始化 |
| `internal/identity`、`internal/security`、`internal/shared` | 用户上下文、安全与公共校验 |
| `internal/filestore` | 文件与图片处理、Range 下载 |
| `docs` | 完整结构规划和路由速查 |

完整依赖方向、扩展约定和事务策略见 [项目结构](docs/architecture.md)，全部端点见 [路由速查](docs/routes.md)。账号与资产约定见 `AUTH-API.md`，团队与分享约定见 `TEAM-API.md`，数据库见 `db.md`。

Gin、GORM 及 MySQL 驱动版本固定在 `go.mod`；已有 Crypto/MySQL 底层依赖保持原版本。Service 不依赖 Gin 或 HTTP，数据库查询集中在 Repository。模型映射现有 schema，不调用 `AutoMigrate`。

## 存储配置

- `VOEX_STATIC_DIR`：默认 `/home/static`，保存按 SHA-256 命名的附件。
- `VOEX_UPLOAD_DIR`：默认 `/home/update`，保存按目录和 UUID 命名的资产，`.incoming` 为临时上传目录。
- `JWT_SECRET_FILE`：默认 `/home/server/.secrets/jwt-key`。保留密钥以维持现有登录会话。

这些目录及 `.env` 不属于代码清理范围。删除附件记录后保留物理哈希文件，避免共享内容及并发上传受到影响。

## 部署

当前进程由服务器已有 PM2 管理，启动的是 Go 二进制，`interpreter` 为 `none`。项目的构建和业务运行不需要 Node.js、npm、pnpm 或 TypeScript。

```sh
cd /home/voex-server
make build
make check
pm2 restart voex-server
pm2 save
pm2 describe voex-server
curl -i http://127.0.0.1:8090/api/auth/me
```

PM2 配置位于 `/home/ecosystem.config.js`，后端入口为 `bin/voex-server`。服务在 SIGINT/SIGTERM 后最多等待 25 秒完成请求，PM2 `kill_timeout` 为 30 秒。未登录时 `/api/auth/me` 返回 `401` JSON。

## 验证与备份

按项目约束不生成、不修改、不运行单元测试。使用格式检查、构建、`go vet`、模块校验及隔离数据库 HTTP 冒烟验证。

2026-09-24 迁移已通过 277 项隔离 HTTP 检查和 14 项原服务只读响应对比；临时数据库与验证进程已清理。证据保存在 `.go-backup-path` 指向的仓库外目录 `validation/`。

项目内旧源码、依赖、构建产物及历史代码副本已删除。仓库外备份包含迁移前代码、数据库快照、进程配置，以及删除前的完整旧项目归档 `legacy-complete-before-removal.tgz`。需要回滚时先在独立目录恢复归档并核对进程配置；不要用数据库快照覆盖正常业务写入。

## Gin/GORM 改写验证与回滚

本次框架改写已保留全部 54 个业务路由及 GET 对应的 HEAD 行为。验证覆盖团队/工作区 159 项、文件/分享/HEAD/Range 94 项、认证互通 45 项，以及现有数据只读对比和 HEAD 30 项；不执行单元测试。

框架改写前的 Go 源码、二进制、环境配置和验证记录保存于 `.gin-backup-path` 指向的仓库外目录。需要紧急切回前一 Go 二进制时：

```sh
cd /home/voex-server
backup=$(cat .gin-backup-path)
install -m 755 "$backup/voex-server" bin/voex-server.rollback
mv bin/voex-server.rollback bin/voex-server
pm2 restart voex-server
pm2 save
```

该操作保留现有数据库、上传内容和登录密钥；源码恢复需另行核对归档或 Git 版本。
