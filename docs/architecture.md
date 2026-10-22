# 项目结构与职责

## 目录与进程

```text
voex-server/
├── cmd/voex-server/          # serve / migrate / schema
├── cmd/voex-updater/         # 独立离线升级进程
├── cmd/voex-release/         # 受信任构建主机的签名 / 验签工具
├── internal/
│   ├── app/、config/         # 服务生命周期、配置读取与依赖组装
│   ├── database/            # 显式迁移、只读 schema 校验及历史严格验证
│   ├── model/、repository/  # 模型与 GORM 数据访问
│   ├── service/             # 权限、事务、版本冲突和业务规则
│   ├── handler/、transport/ # Gin HTTP / JSON / Cookie 和升级代理
│   ├── router/、middleware/ # 路由、认证、CORS、日志与维护模式
│   ├── identity/、security/ # 当前用户、JWT、限流、可信代理
│   ├── filestore/           # 本地不可变文件、暂存、图片校验与 Range
│   ├── updater/             # 发行验签、作业、Docker 操作和备份恢复
│   └── shared/              # 校验、标识及业务错误
├── deploy/                  # Compose、镜像、发行构建、离线安装和 Nginx
├── docs/                    # 架构、路由、数据库、接口及 Docker 运维
└── bin/、dist/              # 不入 Git 的构建产物与离线发行包
```

Compose 将 `db`、`api`、`web`、`updater` 分为四个服务。`web` 使用固定官方 Nginx 镜像并仅发布 HTTP 端口；API、MySQL 和更新器通过内部网络访问。192 服务器的独立验证项目是 `voex-local`，部署根 `/home/voex-deploy`，端口 `18080`，保留旧 PM2 / MySQL。目录及命令见 [DOCKER.md](DOCKER.md)。

## 请求与依赖方向

`router → middleware → handler → service → repository → GORM/MySQL`。

Handler 从 Gin Context 读取输入，把标准 `context.Context`、标识与参数交给 Service，并负责状态码、JSON、Cookie 和流式响应。GET / HEAD 继续各自原有注册与权限；新增健康及系统端点见 [routes.md](routes.md)。

Service 负责业务校验、当前用户权限、团队规则、文档 revision 与事务组合；常规业务不依赖 Gin，不直接执行 Repository 查询。Repository 独占 GORM 数据访问，DTO 投影保留原 NULL、数字和时间语义，模型不直接作为用户响应暴露。

Filestore 由 Service 在授权后调用：`store.go` 解析上传、校验图片和生成相对 key；`local.go` 管理专用 `os.Root`、路径校验和原子发布；`download.go` 使用本地可 seek 文件提供 HEAD / Range / ETag / 条件请求。`/home/static` 不直接公开，普通访问仍经过原项目、文档、资产或分享权限。当前实现没有 R2 读写客户端，也不迁移或删除历史远端对象。

系统升级是明确的部署边界：Gin 已登录路由把允许的 `/api/system/...` 请求代理给固定配置的 updater，替换成服务器内部令牌，剥离浏览器 Cookie；所有已登录用户都可导入和启动升级，不增加管理员角色判断。Updater 持有 Docker socket 和部署目录，因此只能接收受信任的签名包，不能将私钥置于部署环境；业务 API 不直接获得 Docker socket。

## 数据库生命周期

`serve` 只连接并校验既有 schema，不创建数据库、不执行 DDL；`schema` 只读报告状态；只有 `migrate` 可建库、更新迁移账本并执行已知 SQL。没有 `AutoMigrate`。安装和升级均先完成独立迁移，再启动业务 API。

迁移固定按 `001-workspace` 到 `009-ai-conversation-sessions` 前缀顺序执行，SQL 通过 `go:embed` 固定。账本保存 checksum、status 与开始/结束时间；每次 DDL 前持久写入 `dirty`，失败或中断不将已提交的 MySQL DDL 当作已回滚。旧账本仅在列、索引、外键及工作区等校验通过后登记 baseline；部分结构、未知版本、历史缺口、checksum 不匹配和 dirty 均拒绝猜测性修复。详见 [db.md](db.md)。

## 一致性与文件发布

- GORM 池最多 10 个连接，查询与事务绑定请求 context；业务事务使用 REPEATABLE READ。
- 结构写操作先锁 `workspace_state.id=1`，随后检查权限与 revision；事务异常回滚，文档保存冲突保持 HTTP 409。
- 关闭 GORM 对单条操作的冗余自动事务；日期由数据库维护并统一输出 UTC ISO。
- 附件先授权、暂存并发布本地文件，再在写事务内重新授权并建立关联，避免传输占用工作区锁。
- 上传暂存在 `/home/static/.uploads/{process_uuid}`；完整写入并同步后硬链接发布，不覆盖已有对象。附件按 hash 共享，关联删除不删除文件；独立 UUID 资产数据库提交失败时尝试删除。
- 头像与缩略图校验 PNG / JPEG / WebP 后保存原始内容和实际 MIME，不自动转换。

## 维护、升级与恢复

应用包采用 v2：严格四文件 `release.json`、`release.sig`、`images.tar`（仅 API 镜像）、`frontend.tar.gz`（dist）。签名清单包含 `formatVersion=2`、API 固定 tag / ID、`imagesSHA256` 和 `frontendSHA256`，不含 `webImage` / `webImageID`；v1 镜像包被明确拒绝。导入校验 Ed25519、归档摘要、镜像内容、条目类型、路径和版本，再准备前端发布目录；任务、requestId 与只读 watch token 持久保存于部署根 `jobs`，同 requestId / version 返回同任务。

`PrepareFrontend` 安全展开 dist，验证 `index.html` 与 `version.json` 和发行版本，拒绝链接、父路径、重复条目、特殊文件及超限内容；同版本不同内容拒绝覆盖。发布到 `frontend/releases/{version}/app`，将各版本 hashed assets 追加到 `frontend/assets`，同名文件必须内容一致，不覆盖旧资源。准备过程不切换当前前端。

Compose 把整个 `frontend` 父目录只读挂载到固定 Nginx 的 `/srv/voex`，而不是绑定旧 symlink 目标。`frontend/current` 只能是 `releases/{version}` 相对符号链接，经临时链接 + rename 原子切换；Nginx 的 `/app/` root 为 `/srv/voex/current`，`/app/assets/` alias 为 `/srv/voex/assets/`。仅 dist 与公共资产可读，部署根的 jobs、环境、密钥、发行归档及备份仍私有，不放入 frontend。

应用升级先预检来源、API / 前端摘要、schema 与空间，准备新前端并记录旧 `frontend-version`，建立维护标记、停止 API 和后台写入，保存 `db.sql`、`static.tar`、原 `runtime.env`。随后独立执行新版 API `migrate`，仅重建 API，原子切换前端 current，核对 API、schema 与 dist 版本后解除维护。正常升级和回滚均不执行 Web 重建；固定 Nginx、MySQL 与 updater 不随应用包更新。

固定 Nginx 在维护期间继续服务旧入口、累计 hashed assets 和直达 updater 的 `/api/upgrade-status/:jobID`，正常业务仍返回 503。Watch endpoint 只接受对应任务的 `X-Upgrade-Watch-Token`，不授予上传或开始升级权限。

失败恢复先还原旧前端链接，确保迁移容器和 API 停写，必要时恢复实际 SQL 快照，再恢复原环境与 API；健康检查通过后才解除维护。前端版本记录保存在 `backups/{jobID}/frontend-version`，Job 同时记录 `previousFrontendVersion`。不能仅切旧镜像撤销 DDL，也不通过删除累计 assets 或共享静态文件回滚。恢复失败保留维护状态；重启发现未完成任务标记 interrupted，不重放数据库变更。

原 v1 部署不具备上述前端父目录挂载和 v2 updater。必须从受信任的 v2 完整安装包执行一次 `migrate-frontend.sh`：校验签名及兼容来源，提取现有 Web 中的原版 dist，建立 current / assets 布局，备份基础设施配置，归档旧包到 `legacy-releases`，一次性重建 Web / updater / API，但保留应用版本、数据库和密钥。此操作与之后 `0.1.1 → 0.2.0` 应用升级严格分开；失败先恢复原基础设施，恢复不成保持维护。操作步骤见 [DOCKER.md](DOCKER.md)，真实验证结果另行记录。

## 错误与扩展

业务校验通过 `shared.HTTPError` 终止请求；Recovery 返回 `{error: ...}`。存储缺失返回 404，本地 I/O 故障返回 502；响应已开始后读取失败中止 HTTP 传输，避免把截断下载当成功。日志不输出 SQL 参数、密码、token、查询字符串或物理存储路径。

新增业务接口沿用 Repository → Service → Handler 分层，结构变化新增显式迁移并更新 schema 校验；同时维护路由、数据库及领域文档。验证使用 `make fmt`、`make check`、`make build` 和隔离环境 HTTP / 浏览器检查，不生成、修改或运行单元测试。本文记录实现和操作约定，不代表本次发布验证已经完成。
