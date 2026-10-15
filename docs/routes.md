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
| DELETE | `/api/project/:projectKey` | 有项目写权限可删除空项目；有子项时须用 `cascade=true` 确认级联删除。 | `internal/handler/workspace.go` |
| GET/HEAD | `/api/project/:projectKey/tree` | 读取项目、文件夹和文档目录；需要登录。 | `internal/handler/workspace.go` |
| POST | `/api/folder` | 在项目内创建一层文件夹；需要登录。 | `internal/handler/workspace.go` |
| PUT | `/api/folder/:folderKey` | 重命名文件夹；需要登录。 | `internal/handler/workspace.go` |
| DELETE | `/api/folder/:folderKey` | 有所属项目写权限可删除空文件夹；有子项时须用 `cascade=true` 确认级联删除。 | `internal/handler/workspace.go` |
| GET/HEAD | `/api/project/:projectKey/privileges` | 读取项目成员权限策略；需要登录且具有管理权限。 | `internal/handler/workspace.go` |
| PUT | `/api/project/:projectKey/privileges` | 设置继承或指定成员权限；需要登录且具有管理权限。 | `internal/handler/workspace.go` |
| POST | `/api/document` | 创建文档并解析所属项目和文件夹；需要登录。 | `internal/handler/workspace.go` |
| GET/HEAD | `/api/document/:fileKey` | 读取文档正文、元数据和权限；需要登录。 | `internal/handler/workspace.go` |
| GET/HEAD | `/api/document/:fileKey/details` | 重新校验项目读取权限，返回文件详情与路径，不更新最近访问。 | `internal/handler/workspace.go` |
| PUT | `/api/document/:fileKey` | 修改文档名称或按版本保存正文；需要登录。 | `internal/handler/workspace.go` |
| DELETE | `/api/document/:fileKey` | 删除文档及附件关联；需要登录。 | `internal/handler/workspace.go` |
| GET/HEAD | `/api/documents` | 按名称搜索当前用户可读文档的元数据；需要登录。 | `internal/handler/workspace.go` |
| GET/HEAD | `/api/recent-files?limit=N` | 按前端传入的条数读取最近记录；GET 响应写出后按同一条数异步清理。 | `internal/handler/recent.go` |
| GET/HEAD | `/api/recent-files/:fileKey` | 凭本人记录及当前有效的项目/分享授权读取单个文件，GET 更新访问时间。 | `internal/handler/recent.go` |
| GET/HEAD | `/api/recent-files/:fileKey/details` | 重新校验文件读取权限，仅项目可访问时返回路径；不读取正文、附件或更新最近访问。 | `internal/handler/recent.go` |
| PUT | `/api/recent-files/:fileKey` | 重新检查编辑权限，只允许正文和 revision，冲突返回 409。 | `internal/handler/recent.go` |
| DELETE | `/api/recent-files/:fileKey` | 仅移除本人的查看记录，无文件删除副作用。 | `internal/handler/recent.go` |
| GET/HEAD | `/api/recent-files/:fileKey/attachments/:hash` | 重新检查文件读取权限并下载该文件附件。 | `internal/handler/recent.go` |

最近文件 API 合同见 [RECENT-API.md](RECENT-API.md)，文件详情与修改人约定见 [FILE-DETAILS-API.md](FILE-DETAILS-API.md)。

## 项目与文件夹删除

`DELETE /api/project/:projectKey` 和 `DELETE /api/folder/:folderKey` 接受可选查询参数 `cascade`，仅允许单个小写 `true` 或 `false`，默认 `false`。重复值、数组形式、空值及其他值返回 HTTP 400。

- 不带 `cascade` 或传 `cascade=false`：空项目、空文件夹直接删除；存在任意子项时返回 HTTP 409，提示确认连同所有子项一起删除，不修改数据。空文件夹也属于项目的子项。
- 用户确认后以 `cascade=true` 重试：删除目标及其全部后代文件夹、文档和附件关联；文档分享、最近访问记录以及项目成员记录由外键同步清理。文件夹删除范围限定在所属项目及目标子树，不影响同级文件夹。
- 成功返回 HTTP 200 和 `{ "success": true }`。权限校验、子项检查及所有删除均在同一个工作区写锁事务中完成，任何失败均回滚。
- 删除附件关联不会删除共享的 R2 哈希对象；物理对象需由独立清理流程处理。本次不调整目录层级限制或数据库结构。
