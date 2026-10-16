# voex-server

Gin + GORM HTTP 服务：账号认证、团队与项目权限、单层目录、Markdown 文档、附件、资产图片与文件分享。默认监听 `127.0.0.1:8090`，API 前缀 `/api`。

## 环境与运行

- Go 1.27.1。服务器安装于 `/opt/go1.27.1`，`go` 和 `gofmt` 位于 `/usr/local/bin`。
- Make（Linux 构建可用 `apt-get install --no-install-recommends make`）；图片校验使用 Go 标准库与 `golang.org/x/image/webp`，无需安装 libvips。
- MySQL 8，初始表结构见 `internal/database/schema.sql`，最近文件与修改人迁移见 `internal/database/recent-files.sql`、`internal/database/document-details.sql`，连接配置见 `.env.example`。
- Cloudflare R2 私有存储桶及该桶的 S3 API 读写凭据，配置方式见下方「存储配置」。

```sh
cd /home/voex-server
# 仅新部署需要复制示例；已有 .env 时保留现有配置。
cp -n .env.example .env
chmod 600 .env
# 填写 .env 中的 MySQL 和 R2 配置后再启动。
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
| `internal/filestore` | R2 对象存储、临时上传与图片处理、Range 下载 |
| `docs` | 完整结构规划和路由速查 |

完整依赖方向、扩展约定和事务策略见 [项目结构](docs/architecture.md)，全部端点见 [路由速查](docs/routes.md)。账号与资产约定见 `AUTH-API.md`，团队与分享约定见 `TEAM-API.md`，数据库见 `db.md`。

Gin、GORM 及 MySQL 驱动版本固定在 `go.mod`；已有 Crypto/MySQL 底层依赖保持原版本。Service 不依赖 Gin 或 HTTP，数据库查询集中在 Repository。模型映射现有 schema，不调用 `AutoMigrate`。

## 存储配置

文件内容保存到 Cloudflare R2，MySQL 继续保存元数据和权限。使用 AWS SDK for Go v2 的 S3 API，region 固定为 `auto`；桶保持私有，现有 `/api/assets/...`、附件及分享下载均由后端完成权限校验和流式传输。此方式不需要开启桶公共访问或配置浏览器直传 CORS。

1. 在 R2 管理界面创建 API token，选择 **Object Read & Write**，范围限定到 `voex-dev`。
2. 将生成的 **Access Key ID**、**Secret Access Key** 填入本地 `.env`；不要使用普通 Cloudflare API token 替代 S3 密钥。
3. 从桶设置复制 **S3 API endpoint**，按 `.env.example` 填写以下配置。受地域管辖的桶应使用控制台给出的对应 endpoint。

```dotenv
R2_ENDPOINT=https://YOUR_ACCOUNT_ID.r2.cloudflarestorage.com
R2_BUCKET=voex-dev
R2_ACCESS_KEY_ID=YOUR_ACCESS_KEY_ID
R2_SECRET_ACCESS_KEY=YOUR_SECRET_ACCESS_KEY
```

`R2_ENDPOINT` 不包含桶名，不能填写 `r2.dev` 公共访问域名。四项配置均必填，缺失时启动会明确报错。凭据仅保存在被 Git 忽略的 `.env` 或进程环境中。参考 [R2 密钥配置](https://developers.cloudflare.com/r2/api/tokens/)和 [Go SDK 官方示例](https://developers.cloudflare.com/r2/examples/aws/aws-sdk-go/)。

| 内容 | R2 object key |
| --- | --- |
| 文档附件 | `attachments/{sha256}` |
| 普通资产 | `assets/{directory}/{uuid}` |
| 原始头像、缩略图 | `assets/{directory}/{uuid}`，MIME 按实际图片格式保存 |

头像与缩略图只接受可完整解码的静态 PNG、JPEG、WebP，保留 5 MiB / 20 MiB 和 2000 万像素限制；校验通过后原图上传 R2，不缩放、压缩、转码或移除 metadata。历史 `.webp` 对象继续可读，接口返回格式保持现有约定。下载支持 HEAD、单段/多段 Range、ETag 和条件请求；HEAD 只读取对象元数据。超过 16 段的 Range 请求按完整文件响应，避免一次请求触发大量 R2 读取。

- `VOEX_TEMP_DIR`：可选的可信绝对路径，默认系统临时目录。每个进程创建随机私有子目录 `voex-uploads-*`，仅存放上传和图片转换临时文件，请求结束后清理；自定义父目录应仅允许服务用户写入。异常退出留下的临时文件需在确认没有进程使用后手动清理。
- `JWT_SECRET_FILE`：默认 `/home/server/.secrets/jwt-key`。保留密钥以维持现有登录会话。
- 附件按内容哈希复用且禁止覆盖；删除关联记录或上传后的数据库提交失败时保留 R2 对象，避免影响其他文档。未提交的 UUID 资产会尝试删除，失败时记录日志；没有自动孤儿对象清理任务。

### 从本地存储切换

`VOEX_STATIC_DIR`、`VOEX_UPLOAD_DIR` 已不再使用，程序不会自动迁移或回退读取本地旧文件。有历史数据时，应先在原服务器把 `/home/static/{sha256}` 上传为 `attachments/{sha256}`，把 `/home/update/{directory}/{storage_name}` 上传为 `assets/{directory}/{storage_name}`，排除 `.incoming` 临时目录，再切换服务。对象 key 必须与数据库中的 hash、directory、storage_name 对应，数据库无需改表。原目录及 JWT 密钥应保留以便回滚。

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

## 许可证

本项目采用 [PolyForm Noncommercial License 1.0.0](LICENSE)。

个人和团队可以在许可证允许的非商业用途范围内私有化部署、使用、修改和分发本项目。商业用途需另行获得权利人的授权；内网部署或团队内部使用本身不代表非商业用途。

以上仅为说明，完整授权范围、义务及例外以 [LICENSE](LICENSE) 原文为准。
