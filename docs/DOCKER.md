# Docker 离线安装与升级

本说明对应 `deploy/compose.yaml`、`build-release.sh`、`install.sh`、`migrate-frontend.sh` 与 `internal/updater` 的 v2 API 镜像 + 前端 dist 发布流程。192 服务器使用独立目录 `/home/voex-deploy`、Compose 项目 `voex-local`、端口 `18080`，不替换旧 PM2 / MySQL，不导入旧库。v2 的实际转换、升级及回滚验证结果另行记录；本文不预先声明成功或包体积。

## v2 包与信任边界

受信任 Linux amd64 构建机需要 Go（见 `go.mod`）、前端 Node.js / pnpm 依赖、Docker / Compose、Python 3、GNU tar、系统 CA 及提前准备的官方 Nginx / MySQL 基础镜像。目标机只加载编译产物，无需 Go / Node.js / pnpm，也不拉取公网应用镜像。镜像构建 `--network=none` 不代表首次准备依赖无需网络。

```sh
cd /home/voex-server
# 新 v2 版本 0.2.0 只接受从应用版本 0.1.1 升级。
./deploy/build-release.sh 0.2.0 0.1.1
```

默认前端仓库为 `/home/voex`，可用 `VOEX_FRONTEND_DIR` 指定。`VOEX_COMPOSE_BINARY` 指定 Compose plugin，`NGINX_IMAGE` / `NGINX_BASE` / `MYSQL_IMAGE` 指定已准备的基础镜像；部署记录固定镜像 ID，不依赖后续 tag 漂移。

| 输出                             | 内容与用途                                                                                                                 |
| -------------------------------- | -------------------------------------------------------------------------------------------------------------------------- |
| `dist/voex-0.2.0-upgrade.tar.gz` | v2 应用升级包，严格只含下列四个文件                                                                                        |
| `dist/voex-0.2.0-install.tar.gz` | 完整安装包：应用四文件、公钥与验签工具、Compose、Nginx 配置、install / migrate-frontend 脚本、bootstrap 签名及基础设施镜像 |

应用升级包必须且只能包含：

```text
release.json       # formatVersion=2、来源版本、schema 与内容摘要
release.sig        # Ed25519 清单签名
images.tar         # 仅 API 镜像
frontend.tar.gz    # 前端 dist；包含 index.html、version.json、assets 等
```

清单包含 `formatVersion: 2`、`version`、`fromVersions`、`schemaVersion`、`apiImage` / `apiImageID`、`imagesSHA256`、`frontendSHA256`，不再包含 `webImage` / `webImageID`。0.2.0 的来源版本为 `["0.1.1"]`。v2 updater 明确拒绝 v1 三文件镜像包；改名或手动补文件不能代替受信任构建和签名。

完整安装包的 `infrastructure.tar` 才包含固定官方 Nginx、MySQL 和 updater 镜像。正常应用升级包不含 Nginx 镜像，不升级或重建 Nginx，也不自动更新 MySQL / updater。

私钥 `.release-keys/signing.key` 只由受信任构建机保管并独立安全备份，后续发行沿用同一密钥；不得进入包、镜像、仓库或部署端。部署端只持有 `release.pub`。初装包须通过可信交付渠道获取并核对独立包摘要 / 公钥；包内公钥本身不能证明来源。包内不含真实 `.env`、数据库密码、JWT 密钥或内部 updater 令牌。

## 新部署：完整安装

仅适用于还没有 `runtime.env` 的新目录。目标机需要 Linux amd64、Docker Engine / Compose、Python 3、GNU tar 和 curl，磁盘需容纳镜像、前端版本、数据库及备份。确认目录、项目名 `voex-local` 和端口 `18080` 未被其他环境占用。

假设可信包位于 `/home/voex-server/dist/`：

```sh
mkdir -p /home/voex-install/0.2.0
tar -xzf /home/voex-server/dist/voex-0.2.0-install.tar.gz -C /home/voex-install/0.2.0
cd /home/voex-install/0.2.0
./install.sh /home/voex-deploy 18080 0.0.0.0
```

脚本验签并检查 bootstrap、API / 前端归档摘要与镜像 ID，生成私有环境和 JWT 密钥，准备前端目录并切换 current；启动独立 db / updater，维护状态执行 `api migrate`，再启动 API 与固定 Nginx，完成就绪检查后才解除维护。初装需要创建 Nginx 容器；这与后续只重建 API 的应用升级不同。

已有 `runtime.env` 时安装器拒绝覆盖。初装失败应保留环境与维护状态后排查，不删除 `runtime.env` 或 MySQL volume 后重跑。入口是 [192 独立部署](http://192.140.173.58:18080/app/)，示例对外监听 `0.0.0.0`。

同 IP 不同端口共享 Cookie，独立测试应使用独立浏览器配置或无痕窗口，避免影响旧站登录；正式部署使用独立域名。独立新库通过正常注册建立账号。

## 已有 v1 0.1.1：先转换基础设施

旧 v1 Web 镜像内含 dist，旧 updater 不认识 v2 四文件包。已有应用版本 0.1.1 不能直接把 0.2.0 升级包上传给旧 updater，也不能用 `install.sh` 覆盖既有环境。

从可信 `voex-0.2.0-install.tar.gz` 解包后，在包目录单独执行：

```sh
cd /home/voex-install/0.2.0
./migrate-frontend.sh /home/voex-deploy
```

转换前应安排维护，确认没有活动升级或未完成恢复。脚本核对既有公钥、v2 包与 bootstrap 签名、文件摘要、基础设施镜像 ID，以及当前应用版本属于包的 `fromVersions`；已是固定 Nginx 布局则不重复转换。

该操作会：

1. 保存原 `runtime.env`、`compose.yaml` 及已有 Nginx 配置到 `infrastructure-backups/{timestamp}`，进入维护并停止 API / updater。
2. 从现有 Web 容器提取原 0.1.1 dist，核对 `version.json`，建立原版 `frontend/releases/0.1.1/app`、累计 assets 与 current。
3. 将旧 v1 发行目录移动到私有 `legacy-releases`，更换固定 Nginx 的 Compose / 配置和 v2 updater，移除 `VOEX_WEB_IMAGE`，写入固定 `VOEX_NGINX_IMAGE`。
4. 一次性重建 Web / updater / API，核对原应用版本后解除维护。它不执行应用 / 数据库迁移，不更换原 API 版本、数据库、账号、JWT 密钥或静态文件。

转换是一次基础设施操作，允许重建 Nginx；之后的应用升级与回滚不重建 Nginx。转换失败尝试还原原基础设施与移动的发行目录，恢复失败继续维护；不能仅删除维护标记强行放行。

完成转换后应用仍为 0.1.1，再通过工作台导入 `voex-0.2.0-upgrade.tar.gz` 执行正常应用升级。转换结果与后续升级结果必须分别验证和记录。

## 持久化与前端发布布局

部署状态直接在根目录，没有额外 `deployment/` 子层：

```text
/home/voex-deploy/
├── compose.yaml
├── nginx.conf.template
├── release.pub
├── runtime.env                         # 版本、固定镜像引用及密码 / 令牌；私有
├── secrets/jwt-key                     # 登录签名密钥；私有
├── state/maintenance                   # 存在时拒绝业务 API
├── state/updater.lock
├── jobs/{jobID}.json                    # requestId、进度、watchToken；私有
├── imports/                            # 私有上传暂存
├── releases/{version}/                 # 已验签四文件应用包；私有
├── legacy-releases/{version}/          # 一次性转换归档的 v1 包；私有
├── infrastructure-backups/{timestamp}/ # 转换前环境与配置；私有
├── backups/{jobID}/
│   ├── db.sql                          # 完整 SQL 快照；私有
│   ├── static.tar                      # 静态归档，不含 .uploads；私有
│   ├── runtime.env                     # 升级前环境；私有
│   ├── next.env                        # 目标预检环境；私有
│   └── frontend-version                # 升级前的前端版本
└── frontend/                           # 唯一公共 dist 发布根
    ├── releases/{version}/app/          # 不可变版本目录
    ├── current -> releases/{version}   # 仅安全相对 symlink，原子切换
    └── assets/                         # 所有版本 hashed assets 追加，不覆盖

/home/static/
├── attachments/{sha256}
├── assets/{directory}/{storage_name}
└── .uploads/{process_uuid}/

Docker named volume: voex-local_mysql-data
```

Compose 将整个父目录 `frontend` 只读 bind 到固定 Nginx 的 `/srv/voex`，不直接 bind current 的旧目标。Nginx `/app/` 使用 `/srv/voex/current` 为 root，`/app/assets/` alias 到 `/srv/voex/assets/`；current 指向相对 `releases/{version}`，因此请求 `/app/` 进入对应版本的 app 子目录。

只向 frontend 放公共构建内容，不将环境文件、密钥、jobs、发行归档或备份放入其中。部署根机密保持私有且不映射为公共目录。`frontend/assets` 与业务资产 `/home/static/assets` 是不同边界；后者仍由 API 鉴权，不能公开为 Nginx alias。

导入流程先验签并核对 `frontendSHA256`，再调用 `PrepareFrontend` 验证版本、index.html、version.json、条目类型 / 路径 / 大小，拒绝链接、特殊文件、重复条目与父路径。相同版本必须相同内容；assets 同名也必须摘要一致，禁止覆盖旧资源。先完成安全展开与校验，再发布版本目录并追加 assets，导入不改变 current。旧页面所需 hashed assets 因而继续保留，不在回滚时删除。

MySQL volume 与 `/home/static` 独立于容器生命周期；本次不迁移历史 R2、不回退读取 R2，也不删除远端对象。Updater 持有 Docker socket 与私有部署根，不能开放内部端口或接收未签名代码。仅固定 Nginx 发布 `18080`。

## 正常导入与应用升级

1. 确认已有部署已完成 v2 基础设施安装或转换，安全交付受信任构建生成的 `voex-0.2.0-upgrade.tar.gz`。
2. 登录工作台进入“系统升级”。所有已登录用户都可导入和升级，不限管理员；权限端点、页面控件及 multipart 字段 `file` 保持不变。
3. 选择 `.tar.gz` / `.tgz` / `.tar` 升级包并导入，包上限 2 GiB。导入验签 / 校验 API 与前端归档，加载 API 镜像并准备版本化 dist，不切 current，不开始数据库迁移。
4. 选择兼容来源版本，通知使用者停止编辑，再确认升级；0.2.0 要求当前应用版本为 0.1.1。启动结果不明时以同 requestId / version 重试，返回同一任务。
5. 升级预检当前 API / 前端版本、schema 与空间，记录旧 `frontend-version`，维护并停止 API 和后台任务，保存 `db.sql`、`static.tar`、原环境，然后单独运行新版 API `migrate`。
6. 只重建 API，原子切换 `frontend/current`，验证 API、schema 与 dist 版本，完成后解除维护。固定 Nginx、MySQL 与 updater 均不参与本次应用重建。
7. 成功后重新加载页面；失败先核对实际恢复结果及备份，不把错误提示当作原版已经恢复的证明。

固定 Nginx 在维护期间继续服务前端入口、累计 hashed assets 和独立 `/api/upgrade-status/:jobID`；业务 API 仍返回 503。状态监控只使用对应任务的 `X-Upgrade-Watch-Token`，不触发业务登录刷新，也不能导入或启动升级。前端 sessionStorage 保存账号、任务 ID 和只读凭证用于刷新恢复。

## 只读检查与失败恢复

明确选择独立部署配置；不要输出完整 `docker compose config`，其中展开值含机密：

```sh
docker compose --env-file /home/voex-deploy/runtime.env -f /home/voex-deploy/compose.yaml config --quiet
docker compose --env-file /home/voex-deploy/runtime.env -f /home/voex-deploy/compose.yaml ps
docker compose --env-file /home/voex-deploy/runtime.env -f /home/voex-deploy/compose.yaml ps -q web
docker compose --env-file /home/voex-deploy/runtime.env -f /home/voex-deploy/compose.yaml logs --tail 100 api updater
readlink /home/voex-deploy/frontend/current
curl --fail http://127.0.0.1:18080/api/health/live
curl --fail http://127.0.0.1:18080/api/health/ready
curl --fail http://127.0.0.1:18080/app/version.json
docker compose --env-file /home/voex-deploy/runtime.env -f /home/voex-deploy/compose.yaml run --rm --no-deps api schema
```

分别记录一次性转换前后、正常应用升级前后与失败回滚前后的 Web 容器 ID。转换会一次性重建 Web；正常应用升级和回滚应保持固定 Nginx 容器不变。此为验收要求，不能代替实际记录。还需核对 current、安全路径拒绝、旧 hashed assets 可读，以及 API / schema / dist 版本。`live` 只说明进程存活，readiness 可用不代表维护期间允许提前写入。

失败恢复先把 current 原子切回 `backups/{jobID}/frontend-version` 中记录的版本，确认迁移容器和 API 已停止写库；必要时恢复真正的 `db.sql` 快照，再恢复原 `runtime.env` 和 API。固定 Nginx 持续提供状态通道和保留 assets，不通过 `web up` 或删资源恢复。只切 API 镜像不能撤销已经提交的 MySQL DDL。

若任务 interrupted、recovery_required 或自动恢复失败，继续维护并保全任务、备份、环境和日志。由维护人员核对部署、任务与恢复点，恢复匹配数据库 / 环境 / API 与前端链接，完成 schema、版本、文件及业务检查后才解除维护。`db.sql` 含 DROP / CREATE DATABASE，不能向旧服务器库执行；`static.tar` 留作人工灾备，不通过清空共享静态文件实现回滚。

Updater 重启不盲目重放 DDL；数据库 dirty / 未知结构 / checksum 不符须人工处理。前端版本、累计 assets、应用包、基础设施备份和结束任务不自动按容量删除，保留与清理应按恢复策略人工安排。环境、SQL、JWT、内部令牌和作业凭证都不得进入公共下载或发行包。

## v1 历史隔离验证（2026-10-16）

已在独立 `voex-local` 环境完成：14 项 MySQL 迁移/校验集成检查、15 项文件 HTTP 存储检查，以及浏览器发起的 `0.1.0 → 0.1.1` 升级。故意前端版本不匹配的 `0.1.2-rollback-check` 包触发恢复，确认备份后的临时正文改写被还原、附件摘要不变、版本恢复为 `0.1.1`、维护标记移除、重复 requestId 返回同一任务。演练专用发行包已撤下，保留任务与备份审计记录。未运行单元测试，未操作旧 PM2 服务或旧服务器数据库。

交付包为 `dist/voex-0.1.1-install.tar.gz`（独立初装）和 `dist/voex-0.1.1-upgrade.tar.gz`（从 0.1.0 升级）。该记录形成时部署端为 0.1.1；它仅描述 v1 的历史验证与交付，不证明本文 v2 转换、固定 Nginx 或 0.2.0 升级已经通过。

## v2 实测结果（2026-10-16）

当前测试部署已完成一次性基础设施转换，并通过 Web 从 0.1.1 升级到 0.2.0。应用升级包为 24,565,881 字节（23.43 MiB），相比 v1 的 83.44 MiB 缩小 71.9%；实际检查归档仅有 release.json、release.sig、images.tar、frontend.tar.gz，Docker 归档内只有 voex/api:0.2.0。

升级成功和故意后端启动失败后的自动恢复均已验证。两次流程中 Nginx 容器 ID、StartedAt、RestartCount 与转换后的基线完全一致；236 个旧 assets 摘要保持，18 个旧入口/预加载/CSS/懒加载资源通过 HTTP 内容校验。前端 current、API、数据库正文和附件均恢复至 0.2.0，维护标记解除。完整安装包另在隔离端口 18081 通过 32 项初装检查，临时容器/卷已清理。未运行单元测试。

验收记录位于 /tmp/voex-fixed-nginx-validation/（baseline.json、after-upgrade.json、after-rollback.json），初装记录位于 /tmp/voex-v2-installcheck-result.json。演练发行包已撤下，失败任务及备份审计记录保留。
