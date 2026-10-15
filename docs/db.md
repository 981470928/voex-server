# 数据库

Go 服务通过 GORM 和 `gorm.io/driver/mysql` 访问 MySQL 8；底层驱动使用固定版本的 `github.com/go-sql-driver/mysql`。连接参数来自 `.env` 或进程环境：`MYSQL_HOST`、`MYSQL_PORT`、`MYSQL_USER`、`MYSQL_PASSWORD`、`MYSQL_DATABASE`，示例见 `.env.example`。

初始结构见 `internal/database/schema.sql`，后续版本化迁移见 `internal/database/recent-files.sql` 和 `internal/database/document-details.sql`，均通过 `go:embed` 编入服务。不要在文档内保存真实密码。

## 初始化

`internal/database/database.go` 在数据库不存在时创建数据库；空库按嵌入 schema 初始化。验证 `001-workspace`、`002-auth`、`003-teams` 后，在同一个 schema 锁内依次执行尚未应用的 `004-recent-files`、`005-document-details` 迁移，再验证必需字段。新旧数据库都执行这些显式迁移，不回填或改写原有文件数据。

首次初始化需要建库建表权限；已有库首次应用 `004-recent-files` 需要建表和外键引用权限，`005-document-details` 还需要 ALTER 与索引权限，也可由管理员先执行对应迁移文件。完成迁移后的日常运行只需业务表读写权限。连接池最大 10 个连接，事务使用 REPEATABLE READ。

`005-document-details` 通过一次原子 ALTER 添加 nullable `documents.updated_by`、`updated_by_anonymous` 标记及用户外键。若 DDL 已提交、版本写入前进程中断，下次启动会验证已存在的两个字段及该外键，再补记版本。历史行保留 `updated_by=null`、`updated_by_anonymous=false`，表示未记录；匿名分享修改为 null/true；登录修改为用户 ID/false。

## 表与关系

| 表 | 用途 |
| --- | --- |
| `users`、`auth_sessions` | 账号、Argon2id 密码哈希、可撤销登录会话 |
| `teams`、`team_members` | 团队、个人团队、成员角色 |
| `team_invites`、`team_join_requests` | 邀请令牌哈希与入队审批 |
| `projects`、`project_members` | 团队项目、继承或指定成员权限 |
| `folders` | 项目下的单层文件夹 |
| `documents` | Markdown 正文、作者、最后修改人、目录归属和 revision |
| `files` | 文档附件名称、类型、大小和内容哈希 |
| `assets` | UUID 资产、上传目录、作者和图片元数据 |
| `file_shares` | 文档分享令牌哈希、权限和撤销时间 |
| `recent_files` | 每个账号的文件查看记录、查看时名称快照、最后访问时间及已验证分享 ID |
| `workspace_state`、`schema_migrations` | 工作区写锁和 schema 版本记录 |

结构写入在事务内先锁定 `workspace_state.id=1`，再验证权限并修改数据。文档正文写入检查 revision，冲突返回 HTTP 409。

## 文件存储

附件内容保存于私有 R2 桶的 `attachments/{sha256}`；相同内容复用对象，每次上传分别保留关联记录。删除文档或附件仅删除关联记录，对象内容保留，避免共享哈希与并发上传冲突。上传 R2 在数据库写事务之前完成，写事务内仍会重新验证权限。

资产保存于同一桶的 `assets/{directory}/{uuid}`，新图片保留原始内容和实际 MIME；历史转换图片的 `.webp` key 继续有效。数据库保存元数据，文件内容保存在 R2，本地仅暂存上传以校验和传输。此次切换不改变表结构；历史文件须按上述 key 迁移后再切换，配置及迁移约定见 README.md。

数据访问集中在 `internal/repository`，模型位于 `internal/model`。GORM 查询和事务绑定请求 context；不启用 `AutoMigrate`，结构变更仅通过显式版本化迁移执行。

`recent_files` 以 `(user_id, document_id)` 去重，`(user_id, viewed_at, document_id)` 支持最近访问排序。分享仅记录关联 ID，不保存或返回原始 token。删除查看记录不删除文件、附件或分享；删除原文件时通过外键清理对应记录。分享撤销后每次请求重新鉴权，失去访问权限的列表项仅显示查看时保存的名称，不泄露之后修改的元数据。
