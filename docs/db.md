# 数据库

Go 服务通过 GORM / `gorm.io/driver/mysql` 访问 MySQL 8，驱动版本固定于 `go.mod`。连接参数为 `MYSQL_HOST`、`MYSQL_PORT`、`MYSQL_USER`、`MYSQL_PASSWORD`、`MYSQL_DATABASE`；不在文档保存真实密码。Docker 的新库由独立 Compose `db` 服务提供，named volume 为 `voex-local_mysql-data`，不导入或替换原服务器 MySQL。

## 命令分离

| 命令                        | 行为                                                              |
| --------------------------- | ----------------------------------------------------------------- |
| `voex-server serve`（默认） | 连接已有库并校验 schema，拒绝不兼容状态；不建库、不执行 DDL       |
| `voex-server schema`        | 只读 JSON，报告当前 / 目标版本、兼容性、待迁移和 dirty / 未知状态 |
| `voex-server migrate`       | 取得 schema 锁后执行固定已知迁移；唯一允许建库与 DDL 的入口       |

首次安装、版本升级与直接启动二进制都必须先运行 `migrate`。`schema` 对已识别的不兼容状态仍可能正常输出 JSON，不能只用退出码判断就绪，应检查 `compatible`、`can_migrate`、`needs_migration`、`dirty`、`unknown_versions` 及 `error`。完成迁移后的日常服务只需业务表读写及必要结构读取权限；迁移独立需要相应建库 / 建表 / ALTER / 索引 / 外键权限。

迁移采用同连接 MySQL `GET_LOCK('voex-go-schema',30)` 排他锁。连接池最多 10 个连接，查询和事务绑定请求 context，业务事务使用 REPEATABLE READ；没有 GORM `AutoMigrate`。

## 显式迁移与 baseline

SQL 以 `go:embed` 固定在二进制内，顺序如下：

| 版本                           | 源文件 / 用途                                       |
| ------------------------------ | --------------------------------------------------- |
| `001-workspace`                | `schema.sql`，基础工作区                            |
| `002-auth`                     | 历史联合 bootstrap 的认证校验                       |
| `003-teams`                    | 历史联合 bootstrap 的团队校验                       |
| `004-recent-files`             | `recent-files.sql`，最近访问                        |
| `005-document-details`         | `document-details.sql`，最后修改人                  |
| `006-trash`                    | `trash.sql`，软删除与到期                           |
| `007-ai-agent`                 | `ai.sql`，模型与审阅应用                            |
| `008-ai-conversation-history`  | `ai-history.sql`，对话轮次                          |
| `009-ai-conversation-sessions` | `ai-sessions.sql`，独立对话会话                      |
| `010-user-config`              | `user-config.sql`，用户自定义配置；当前 LatestVersion |

`schema_migrations` 保存 `version`、`checksum`、`status`、`applied_at`、`started_at`、`finished_at`、`last_error`。Checksum 包含迁移版本及对应嵌入源文本，已应用 SQL 不能原地修改。

空库走已知 bootstrap 与后续迁移。旧版只有 version / applied_at 的账本不会被直接信任：先严格校验完整已知前缀、列类型和 nullable/default、索引、外键、引擎及工作区约束，再升级账本元数据并登记 `baseline`。不会把已有未知业务表自动认领成一个新库，也不假定缺记录的半套 DDL 已经成功。

以下情况 `serve` / `migrate` 均拒绝继续：未知迁移、历史版本缺口、checksum 不符、未记录但已存在的部分结构、账本元数据不完整、必需结构校验失败或任一 dirty 记录。必须人工核对原始版本与备份后恢复或修复，不能仅删记录、伪造 checksum 或修改状态来跳过校验。

每个新迁移在 DDL 前先提交 `dirty` 记录，完成 SQL 和结构校验后才标为 `applied`。MySQL DDL 可能已提交，失败不等于事务回滚；中断或连接丢失仍保持 dirty 阻止自动重跑。升级器因此保存并在必要时恢复完整 SQL 快照，而不是只切回旧镜像。

`005-document-details` 为历史文档保留 `updated_by=null`、`updated_by_anonymous=false`，表示未记录；匿名分享修改为 null / true，登录修改为用户 ID / false。新流程遇到未登记的现成字段时会拒绝自动收养，不能再按旧的“重启后补记版本”方式处理。

## 表与业务一致性

| 表                                                  | 用途                                                   |
| --------------------------------------------------- | ------------------------------------------------------ |
| `users`、`auth_sessions`                            | 账号、Argon2id 密码哈希、用户自定义 JSON 配置与登录会话 |
| `teams`、`team_members`                             | 团队、个人团队与成员角色                               |
| `team_invites`、`team_join_requests`                | 邀请凭证哈希与入队审批                                 |
| `projects`、`project_members`、`folders`            | 团队项目、授权与单层文件夹                             |
| `documents`                                         | Markdown 正文、目录、最后修改人、revision 与软删除状态 |
| `files`、`assets`                                   | 文档附件关联与 UUID 资产元数据                         |
| `file_shares`                                       | 文档分享凭证哈希、编辑权限与撤销                       |
| `recent_files`                                      | 按账号和文档去重的查看记录及已验证分享 ID              |
| `ai_models`、`ai_review_applications`               | 模型配置与审阅应用记录                                 |
| `ai_conversation_turns`、`ai_conversation_sessions` | 对话轮次、独立会话和不可变审阅快照                     |
| `workspace_state`、`schema_migrations`              | 工作区写锁与迁移账本                                   |

`users.user_config` 为非空 JSON 列，默认 `(JSON_OBJECT())`；已有账号通过迁移得到空对象，新注册账号使用数据库默认值。配置只按当前用户 ID 读取和修改，合并事务仅锁对应 users 行，不使用全局工作区锁；内部结构由前端决定，接口见 [USER-CONFIG-API.md](USER-CONFIG-API.md)。

结构写入事务先锁 `workspace_state.id=1`，再验证权限和修改数据；正文 revision 冲突保持 409。最近记录不保存或返回原始分享 token，撤销分享后每次请求重新授权；移除记录不删除原文、附件或分享。

## 文件与备份边界

文件内容保存于主机 `VOEX_STORAGE_ROOT`（默认 `/home/static`），元数据和关联在 MySQL。附件为 `attachments/{sha256}`，资产为 `assets/{directory}/{storage_name}`，临时上传位于 `.uploads`；附件关联删除不删除共享内容。HTTP 读写权限保持原授权接口，不公开静态目录。

本次不迁移历史 R2 对象、不回退读 R2，也不删除远端数据或改写历史元数据。旧内容未出现在本地时会按原下载接口返回不存在，独立部署不导入旧库。

升级前停止 API 与后台任务，备份至部署根 `backups/{jobID}`：`db.sql`、`static.tar`、`runtime.env`，并保存目标预检环境 `next.env`；静态归档排除 `.uploads`。SQL 使用一致性导出并包含建库 / 删除原库语句，恢复会覆盖该独立部署数据库，必须在停写、核对任务与目标后操作。备份内有密码、令牌和业务数据，严禁放入公共目录或发行包。

升级失败可能恢复 SQL 快照、原运行环境和 API / Web；恢复失败或 updater 重启中断则保留维护，人工检查后再恢复。静态内容不可变，自动回滚不会通过删文件清空共享存储。流程及运维命令见 [DOCKER.md](DOCKER.md)。本文不宣称迁移或真实恢复验证已经通过。
