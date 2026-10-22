# voex-server

Gin + GORM HTTP 服务：账号认证、团队与项目权限、单层目录、Markdown 文档、附件、资产图片与文件分享。API 前缀 `/api`；独立二进制默认监听 `127.0.0.1:8090`。

当前文件后端为主机本地 `/home/static`。推荐通过已签名离线包部署；192 服务器验证环境使用独立 Compose 项目 `voex-local`、目录 `/home/voex-deploy`、HTTP 端口 `18080`，不替换旧 PM2 进程或旧 MySQL。初装、离线升级及恢复步骤见 [Docker 部署](docs/DOCKER.md)。

## 源码环境与运行

- Go 1.27.1，版本依赖固定在 `go.mod` / `go.sum`。服务器已安装的 `go` / `gofmt` 位于 `/usr/local/bin`。
- Make、MySQL 8；图片校验使用 Go 标准库与 `golang.org/x/image/webp`，无需 libvips。
- 服务用户须能读写专用绝对路径 `VOEX_STORAGE_ROOT`，默认 `/home/static`；目录不得通过符号链接逃逸到其他位置。
- 原生 Go API 不依赖 Node.js；构建包含前端的离线发行包需要前端仓库、Node.js / pnpm、Docker / Compose，见 Docker 文档。

以下命令仅用于已明确指定的开发数据库；新环境填写 `.env`，已有配置应保留。运行服务前必须单独执行迁移，`serve` 不建库、不执行 DDL。

```sh
cd /home/voex-server
cp -n .env.example .env
chmod 600 .env
# 编辑 .env，设置开发 MySQL、VOEX_STORAGE_ROOT 和 JWT_SECRET_FILE。
make build
make check
./bin/voex-server schema
./bin/voex-server migrate
./bin/voex-server serve
```

`schema` 输出兼容性 JSON，需要检查 `compatible` / `can_migrate` / `dirty`，不能只看进程退出码。已有业务库迁移前先停写并备份；不要将本例指向旧线上数据库。迁移记录、旧库 baseline 与 dirty 处理见 [数据库](docs/db.md)。

`.env` 自动加载，进程环境变量优先；`VOEX_ENV_FILE` 可指定其他配置文件。默认模块代理不可达时可使用 `GOPROXY=https://goproxy.cn,direct go mod download`，保持 Go checksum 校验。`make build` 原子替换 `bin/voex-server`；`make fmt` 格式化 `cmd` 和 `internal`；`make check` 执行 `go vet` 与 `go mod verify`，不运行单元测试。

## 项目结构

| 目录                                                        | 职责                                                     |
| ----------------------------------------------------------- | -------------------------------------------------------- |
| `cmd/voex-server`                                           | `serve`、独立 `migrate` 与只读 `schema` 命令             |
| `cmd/voex-updater`、`internal/updater`                      | 持久化离线发行包、备份、升级与失败恢复                   |
| `cmd/voex-release`                                          | Ed25519 发行签名与校验工具                               |
| `internal/app`、`internal/config`                           | 依赖组装、服务生命周期和环境配置                         |
| `internal/router`、`internal/middleware`                    | Gin 路由、登录、CORS、日志、维护模式                     |
| `internal/handler`、`internal/transport`                    | 参数解析、HTTP / JSON / Cookie 与升级代理                |
| `internal/service`、`internal/repository`、`internal/model` | 业务规则、权限、事务及 GORM 数据访问                     |
| `internal/database`                                         | 只读 schema 校验、显式版本化迁移与 checksum / dirty 记录 |
| `internal/filestore`                                        | 本地不可变文件、私有暂存、原图校验与 Range 下载          |
| `deploy`                                                    | 离线构建、Compose、安装脚本及 Nginx 配置                 |
| `docs`                                                      | 架构、数据库、路由及 Docker 运维说明                     |

依赖方向与事务规则见 [架构](docs/architecture.md)，端点见 [路由](docs/routes.md)，账号与团队接口见 `docs/AUTH-API.md` / `docs/TEAM-API.md`。模型不调用 `AutoMigrate`。

## 本地文件存储

只需配置本地存储根，不再配置 R2 凭据：

```dotenv
VOEX_STORAGE_ROOT=/home/static
```

| 内容                   | 主机路径                                         |
| ---------------------- | ------------------------------------------------ |
| 文档附件               | `/home/static/attachments/{sha256}`              |
| 普通资产、头像与缩略图 | `/home/static/assets/{directory}/{storage_name}` |
| 每进程上传暂存         | `/home/static/.uploads/{process_uuid}/...`       |

文件内容在本地，MySQL 保存元数据、文档关联及权限。`local.go` 通过受限文件系统根和路径校验访问文件；上传完整写入并同步后以不覆盖的硬链接发布，附件仍按 hash 复用。未提交的 UUID 资产尝试删除；共享附件不随关联删除，避免影响其他文档，没有自动孤儿文件清理任务。异常退出遗留的 `.uploads` 文件不会被自动扫描删除，只能在确认没有进程使用后人工处理。

头像和缩略图只接受可完整解码的静态 PNG / JPEG / WebP，沿用 5 MiB / 20 MiB 与 2000 万像素限制；保存原始字节和实际 MIME，不自动转换 WebP。文件仍必须经过既有 API 鉴权，主机目录不映射为 Nginx 公共目录；HEAD、单段/多段 Range、ETag 与条件请求保留。超过 16 段 Range 按完整响应处理。

本次不迁移历史 R2 文件、不回退读取 R2，也不删除远端桶或对象。旧数据库中已有的元数据不代表对应内容已经出现在本地：缺失本地文件按原下载接口返回不存在。需要历史内容时应另行制定迁移方案，不能把改配置当作迁移已完成。旧远端数据与旧环境继续保留，独立 Docker 环境不导入旧数据库。

`JWT_SECRET_FILE` 仍是登录签名密钥路径；原生运行默认 `/home/server/.secrets/jwt-key`，Docker 使用部署根下 `secrets/jwt-key`。保留密钥才能维持既有会话。`VOEX_TEMP_DIR`、R2 配置和旧 `/home/update` 不属于当前存储路径配置。

## 离线安装与后台升级

受信任构建主机生成 v2 发行包：

```sh
cd /home/voex-server
# 0.2.0 明确允许从应用版本 0.1.1 升级。
./deploy/build-release.sh 0.2.0 0.1.1
```

输出为 `dist/voex-0.2.0-install.tar.gz` 与 `dist/voex-0.2.0-upgrade.tar.gz`。应用升级包必须且只能包含 `release.json`、`release.sig`、`images.tar`（仅 API 镜像）、`frontend.tar.gz`（前端 dist）。签名清单使用 `formatVersion: 2` 和 `frontendSHA256`，不再包含 `webImage` / `webImageID`。

构建私钥 `.release-keys/signing.key` 只留在受信任构建主机，不进入包、镜像或部署环境。完整安装包额外包含 Compose、Nginx 配置、安装及转换脚本、公钥、验签工具和基础设施镜像（固定官方 Nginx、MySQL、updater），不含真实 `.env`。全新环境解包后执行 `./install.sh /home/voex-deploy 18080 0.0.0.0`，入口为 `http://192.140.173.58:18080/app/`。

已有 v1 应用版本 0.1.1 必须先从受信任 v2 完整安装包中执行 `./migrate-frontend.sh /home/voex-deploy`。这次基础设施转换保留应用版本、数据库、密钥和账号，一次性重建 Web / updater / API，将旧发行包移入 `legacy-releases` 并保存 `infrastructure-backups`；转换不是应用升级。随后才在工作台导入 0.2.0 v2 升级包。详细前置条件及恢复方式见 [Docker 部署](docs/DOCKER.md)。

工作台“系统升级”对所有已登录用户开放，不限管理员；服务端仍强制校验签名、公钥、API 镜像及前端归档摘要和来源版本。导入准备 `frontend/releases/{version}/app` 并追加不可变 hashed assets，不切换当前页面；确认升级后迁移数据库、重建 API，再原子切换 `frontend/current` 相对链接。固定官方 Nginx 只在完整安装或专门基础设施转换时配置，正常应用升级及回滚不重建 Nginx，也不自动升级 MySQL / updater。

迁移或健康检查失败时还原备份中的前端版本、实际数据库快照、原环境与 API；恢复失败保持维护模式，更新器重启不会盲目重跑中断任务。业务 API 合同和页面控件不因包格式变化而增加。

原 `/home/ecosystem.config.js` 与旧 PM2 后端属于保留环境，本次独立 Docker 安装不需要 `pm2 restart` 或覆盖旧二进制。直接运行新版二进制时同样必须先在目标数据库完成 `migrate`。

## 验证与备份

按项目约束不生成、不修改、不运行单元测试。使用格式检查、构建、`go vet`、模块校验及隔离数据库 HTTP 冒烟验证。

2026-09-24 迁移已通过 277 项隔离 HTTP 检查和 14 项原服务只读响应对比；临时数据库与验证进程已清理。证据保存在 `.go-backup-path` 指向的仓库外目录 `validation/`。

项目内旧源码、依赖、构建产物及历史代码副本已删除。仓库外备份包含迁移前代码、数据库快照、进程配置，以及删除前的完整旧项目归档 `legacy-complete-before-removal.tgz`。需要回滚时先在独立目录恢复归档并核对进程配置；不要用数据库快照覆盖正常业务写入。

## Gin/GORM 改写验证与回滚

本次框架改写已保留全部 54 个业务路由及 GET 对应的 HEAD 行为。验证覆盖团队/工作区 159 项、文件/分享/HEAD/Range 94 项、认证互通 45 项，以及现有数据只读对比和 HEAD 30 项；不执行单元测试。

框架改写前的 Go 源码、二进制、环境配置和验证记录保存于 `.gin-backup-path` 指向的仓库外目录。以下是旧 Gin/GORM 改写的历史回滚记录，不是当前 Docker 升级流程。旧二进制必须与恢复后的 schema 匹配；不能用只切换镜像或二进制的方式撤销已提交的 MySQL DDL。核对数据库版本及备份后，历史 PM2 环境的二进制切回方式为：

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
