# 数据库

Go 服务通过 `database/sql` 和 `github.com/go-sql-driver/mysql` 访问 MySQL 8。连接参数来自 `.env` 或进程环境：`MYSQL_HOST`、`MYSQL_PORT`、`MYSQL_USER`、`MYSQL_PASSWORD`、`MYSQL_DATABASE`，示例见 `.env.example`。

完整、可执行的结构以 `schema.sql` 为准，通过 `go:embed` 编入服务。不要在文档内保存真实密码。

## 初始化

`db.go` 在数据库不存在时创建数据库；空库按嵌入 schema 初始化。已有库验证 `001-workspace`、`002-auth`、`003-teams` 标记及必需字段，不改写现有业务数据。旧库迁移未完成时启动失败，需要先完成迁移。

首次初始化需要建库建表权限。已有数据库日常运行使用具有相应业务表读写权限的账号即可。连接池最大 10 个连接，事务使用 REPEATABLE READ。

## 表与关系

| 表 | 用途 |
| --- | --- |
| `users`、`auth_sessions` | 账号、Argon2id 密码哈希、可撤销登录会话 |
| `teams`、`team_members` | 团队、个人团队、成员角色 |
| `team_invites`、`team_join_requests` | 邀请令牌哈希与入队审批 |
| `projects`、`project_members` | 团队项目、继承或指定成员权限 |
| `folders` | 项目下的单层文件夹 |
| `documents` | Markdown 正文、作者、目录归属和 revision |
| `files` | 文档附件名称、类型、大小和内容哈希 |
| `assets` | UUID 资产、上传目录、作者和图片元数据 |
| `file_shares` | 文档分享令牌哈希、权限和撤销时间 |
| `workspace_state`、`schema_migrations` | 工作区写锁和 schema 版本记录 |

结构写入在事务内先锁定 `workspace_state.id=1`，再验证权限并修改数据。文档正文写入检查 revision，冲突返回 HTTP 409。

## 文件存储

附件内容保存于 `VOEX_STATIC_DIR/{sha256}`，默认 `/home/static`；相同内容复用物理文件，每次上传分别保留关联记录。删除文档或附件仅删除关联记录，物理内容保留，避免共享哈希与并发上传冲突。

资产保存于 `VOEX_UPLOAD_DIR/{directory}/{uuid}`，处理后的图片使用 `.webp` 后缀，默认根目录 `/home/update`。数据库保存元数据，文件内容保存在磁盘。
