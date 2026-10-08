# 路由速查

路由实现位于 `internal/handler`，每条注册语句前有简短说明；`internal/router` 统一组装公开、登录与可选登录路由组。

GET 与 HEAD 使用相同权限规则，HEAD 不返回响应正文。分享端点需要 `X-Share-Token`；登录端点接受 Bearer 或对应 Cookie。

| 方法 | 路径 | 用途与权限 | 路由文件 |
| --- | --- | --- | --- |
| POST | `/api/auth/account-availability` | 检查账号是否可用，公开且限流。 | `internal/handler/auth.go` |
| POST | `/api/auth/register` | 注册账号、个人团队和登录会话，公开且限流。 | `internal/handler/auth.go` |
| POST | `/api/auth/login` | 校验密码并创建登录会话，公开且限制失败次数。 | `internal/handler/auth.go` |
| POST | `/api/auth/refresh` | 使用刷新 Cookie 续签访问令牌，无需有效访问令牌。 | `internal/handler/auth.go` |
| POST | `/api/auth/logout` | 撤销当前会话并清理 Cookie，可重复调用。 | `internal/handler/auth.go` |
| GET/HEAD | `/api/auth/me` | 获取当前用户资料，需要登录。 | `internal/handler/auth.go` |
| PATCH | `/api/auth/me` | 修改当前用户资料与头像，需要登录。 | `internal/handler/auth.go` |
| GET | `/api/document/:fileKey/shares` | 有文档分享权限的用户读取分享记录。 | `internal/handler/shares.go` |
| HEAD | `/api/document/:fileKey/shares` | 校验分享权限并返回分享列表响应头。 | `internal/handler/shares.go` |
| POST | `/api/document/:fileKey/shares` | 有文档分享权限的用户创建只读或编辑链接。 | `internal/handler/shares.go` |
| DELETE | `/api/document/:fileKey/shares/:id` | 项目管理员或分享创建者撤销链接。 | `internal/handler/shares.go` |
| GET | `/api/shared-file` | 持有效分享令牌的匿名或登录用户读取指定文档。 | `internal/handler/shares.go` |
| HEAD | `/api/shared-file` | 校验有效分享令牌后返回文档响应头。 | `internal/handler/shares.go` |
| PUT | `/api/shared-file` | 具备编辑能力的链接持有者按版本号保存正文。 | `internal/handler/shares.go` |
| GET | `/api/shared-file/attachments/:hash` | 持有效分享令牌的用户读取该文档附件。 | `internal/handler/shares.go` |
| HEAD | `/api/shared-file/attachments/:hash` | 校验分享令牌后读取附件响应头。 | `internal/handler/shares.go` |
| GET | `/api/shared-file/creator-avator` | 持有效分享令牌的用户读取该文档创建者头像。 | `internal/handler/shares.go` |
| HEAD | `/api/shared-file/creator-avator` | 校验分享令牌后读取创建者头像响应头。 | `internal/handler/shares.go` |
| POST | `/api/assets/upload` | 登录用户上传资产，头像和缩略图转换为 WebP。 | `internal/handler/storage.go` |
| GET | `/api/assets/:id` | 创建者或同团队成员读取允许访问的头像资产。 | `internal/handler/storage.go` |
| HEAD | `/api/assets/:id` | 通过相同权限校验后返回资产响应头。 | `internal/handler/storage.go` |
| POST | `/api/upload` | 有文档写权限的登录用户上传附件。 | `internal/handler/storage.go` |
| GET | `/api/files/:fileKey` | 有文档读权限的登录用户列出附件。 | `internal/handler/storage.go` |
| HEAD | `/api/files/:fileKey` | 校验文档读权限并返回附件列表响应头。 | `internal/handler/storage.go` |
| GET | `/api/download/:fileKey/:hash` | 有文档读权限的用户下载附件，支持 Range。 | `internal/handler/storage.go` |
| HEAD | `/api/download/:fileKey/:hash` | 有文档读权限的用户读取附件响应头。 | `internal/handler/storage.go` |
| GET | `/api/upload-progress/:fileKey/:hash` | 有文档读权限的用户查询上传完成状态。 | `internal/handler/storage.go` |
| HEAD | `/api/upload-progress/:fileKey/:hash` | 校验文档读权限并返回进度响应头。 | `internal/handler/storage.go` |
| DELETE | `/api/attachment/:fileKey/:hash` | 有文档写权限的用户删除附件记录。 | `internal/handler/storage.go` |
| POST | `/api/team-invites/inspect` | 公开检查团队邀请链接，限制请求频率。 | `internal/handler/teams.go` |
| GET/HEAD | `/api/teams` | 需登录，列出当前用户已加入的团队。 | `internal/handler/teams.go` |
| POST | `/api/teams` | 需登录，创建普通团队并成为所有者。 | `internal/handler/teams.go` |
| GET/HEAD | `/api/teams/:key` | 需团队成员身份，查看团队详情和权限。 | `internal/handler/teams.go` |
| PATCH | `/api/teams/:key` | 需团队所有者或管理员，修改团队名称。 | `internal/handler/teams.go` |
| DELETE | `/api/teams/:key` | 仅普通团队所有者可删除已清空项目的团队。 | `internal/handler/teams.go` |
| POST | `/api/teams/:key/transfer` | 仅普通团队所有者可转让给现有成员。 | `internal/handler/teams.go` |
| GET/HEAD | `/api/teams/:key/members` | 需团队成员身份，列出团队成员和角色。 | `internal/handler/teams.go` |
| PATCH | `/api/teams/:key/members/:userId` | 仅所有者可设置成员或管理员角色。 | `internal/handler/teams.go` |
| DELETE | `/api/teams/:key/members/:userId` | 成员可退出，管理者按权限移除成员。 | `internal/handler/teams.go` |
| GET/HEAD | `/api/teams/:key/invites` | 需所有者或管理员，查看团队邀请记录。 | `internal/handler/teams.go` |
| POST | `/api/teams/:key/invites` | 需所有者或管理员，生成团队邀请链接。 | `internal/handler/teams.go` |
| DELETE | `/api/teams/:key/invites/:id` | 需所有者或管理员，撤销团队邀请。 | `internal/handler/teams.go` |
| POST | `/api/team-join-requests` | 需登录，通过团队编号或邀请提交加入申请。 | `internal/handler/teams.go` |
| GET/HEAD | `/api/team-join-requests` | 需登录，查看本人提交的团队加入申请。 | `internal/handler/teams.go` |
| GET/HEAD | `/api/teams/:key/join-requests` | 需所有者或管理员，查看团队待审申请。 | `internal/handler/teams.go` |
| PATCH | `/api/teams/:key/join-requests/:id` | 需所有者或管理员，通过或拒绝申请。 | `internal/handler/teams.go` |
| GET/HEAD | `/api/projects` | 列出当前用户可访问的项目；需要登录。 | `internal/handler/workspace.go` |
| POST | `/api/workspace/initialize` | 初始化并读取指定团队工作区；需要登录。 | `internal/handler/workspace.go` |
| POST | `/api/project` | 创建项目及默认文件夹；需要登录。 | `internal/handler/workspace.go` |
| PUT | `/api/project/:projectKey` | 重命名有写入权限的项目；需要登录。 | `internal/handler/workspace.go` |
| DELETE | `/api/project/:projectKey` | 删除已清空的项目；需要登录。 | `internal/handler/workspace.go` |
| GET/HEAD | `/api/project/:projectKey/tree` | 读取项目、文件夹和文档目录；需要登录。 | `internal/handler/workspace.go` |
| POST | `/api/folder` | 在项目内创建一层文件夹；需要登录。 | `internal/handler/workspace.go` |
| PUT | `/api/folder/:folderKey` | 重命名文件夹；需要登录。 | `internal/handler/workspace.go` |
| DELETE | `/api/folder/:folderKey` | 删除已清空的文件夹；需要登录。 | `internal/handler/workspace.go` |
| GET/HEAD | `/api/project/:projectKey/privileges` | 读取项目成员权限策略；需要登录且具有管理权限。 | `internal/handler/workspace.go` |
| PUT | `/api/project/:projectKey/privileges` | 设置继承或指定成员权限；需要登录且具有管理权限。 | `internal/handler/workspace.go` |
| POST | `/api/document` | 创建文档并解析所属项目和文件夹；需要登录。 | `internal/handler/workspace.go` |
| GET/HEAD | `/api/document/:fileKey` | 读取文档正文、元数据和权限；需要登录。 | `internal/handler/workspace.go` |
| PUT | `/api/document/:fileKey` | 修改文档名称或按版本保存正文；需要登录。 | `internal/handler/workspace.go` |
| DELETE | `/api/document/:fileKey` | 删除文档及附件关联；需要登录。 | `internal/handler/workspace.go` |
| GET/HEAD | `/api/documents` | 按名称搜索当前用户可读文档的元数据；需要登录。 | `internal/handler/workspace.go` |
