# 项目结构与职责

## 目录

```text
voex-server/
├── cmd/voex-server/main.go    # 唯一可执行入口
├── internal/
│   ├── app/                  # 配置、数据库、存储、业务依赖组装，优雅停止
│   ├── config/               # .env 读取与配置结构
│   ├── database/             # GORM/MySQL 连接、受控初始化、嵌入 schema.sql
│   ├── model/                # 现有表对应的 GORM 模型
│   ├── repository/           # 按 auth/teams/workspace/storage/shares 划分数据访问
│   ├── service/              # 对应领域的业务、权限、事务和版本冲突规则
│   ├── handler/              # Gin 路由注册、参数解析、HTTP 响应
│   ├── router/               # 公开、已登录、可选登录三组路由的组装
│   ├── middleware/           # Gin 认证、Origin/CORS、日志、异常恢复
│   ├── transport/            # JSON 输入、Cookie 与请求令牌解析
│   ├── identity/             # context 中的当前登录用户
│   ├── security/             # 签名密钥、限流、可信代理识别
│   ├── filestore/            # 私有 R2 对象、临时上传、Go 图片校验、下载 Range
│   └── shared/               # 公共值转换、标识、校验和业务错误
├── docs/
│   ├── architecture.md       # 分层规则与扩展方式
│   └── routes.md             # 全部路由与权限速查
├── .env.example
├── Makefile
├── go.mod / go.sum
├── README.md
├── AUTH-API.md / TEAM-API.md / db.md
└── bin/voex-server           # 构建产物，不入 Git
```

## 请求流与依赖方向

`router → middleware → handler → service → repository → GORM/MySQL`。

Handler 从 Gin Context 读取参数，将标准 `context.Context`、标识和输入值交给 Service，负责状态码、JSON、Cookie 和流式响应。每条路由注册前都有用途及鉴权要求的简短中文注释。GET 的 HEAD 行为显式保留。

Service 负责业务校验、当前用户权限、团队规则、文档 revision 和事务组合；不依赖 Gin 或 `net/http`，不包含 SQL 查询。共享业务通过同一个 `Service` 的领域方法复用，不创建无必要的接口层。

Repository 独占数据查询与修改，使用 GORM `Model`、`Table`、`Where`、`Joins`、`Create`、`Updates`、`Delete`。跨表 DTO 采用 map 投影保留已有 JSON 的 NULL、数字及时间语义；模型映射表结构，数据库模型不直接序列化为用户响应，避免密码哈希等内部字段暴露。SQL 表达式限于数据访问及 schema 初始化。

Filestore 负责 R2 对象和图像；Service 先校验业务权限，再调用文件存储和 Repository。`store.go` 负责上传解析、图片处理和对象 key，`r2.go` 负责 S3 客户端及对象写入/回滚，`download.go` 通过 HeadObject 和惰性 Range GET 提供 HTTP 下载，保留 HEAD、多段 Range 与安全响应头。文件内容不再持久化到本地，只在请求期间暂存上传和图像转换文件。

## 数据一致性

- GORM 连接池最多 10 个连接，查询与事务绑定请求 context。
- 业务事务统一使用 REPEATABLE READ；结构写操作先锁 `workspace_state.id=1`，随后进行权限和 revision 检查。
- 事务内异常会回滚；文档并发保存只有匹配当前 revision 的请求成功。
- 单条写入由 MySQL 原子执行，多步骤业务使用显式事务；关闭 GORM 自动包裹单条操作的冗余事务。
- 保留现有数据库时间与 NULL 语义；日期由数据库维护，响应统一转成 UTC ISO 格式。
- 不调用 `AutoMigrate`。空库使用嵌入的 `schema.sql` 初始化；新旧库在 schema 锁内执行缺少的显式版本化迁移（`004-recent-files`、`005-document-details`），再校验必要字段，不做猜测性改表。
- 附件先授权并上传 R2，再在写事务中重新授权并创建关联，避免网络上传占用全局工作区锁。哈希对象不可覆盖且不随关联记录删除；未提交的 UUID 资产独立尝试删除。

## 鉴权与错误处理

公开组用于注册、登录、刷新、退出、账号检测和邀请预览；已登录组负责团队、工作区、资产与附件；可选登录组用于分享访问，仍需有效分享令牌。项目权限与分享撤销在 Service 中检查。

JWT 密钥、Argon2id 参数、refresh session、Cookie 路径保持兼容。Origin/CORS 与限流在请求边界执行，IPv6 限流按 /56 聚合。

业务校验通过 `shared.HTTPError` 终止当前请求；Gin Recovery 统一转为 `{error: ...}`。数据库错误映射为原 HTTP 状态及中文提示，日志不记录 SQL 参数、密码、token 或查询字符串。

R2 对象不存在返回 404，存储调用故障返回 502。若下载响应已经开始而上游断流，转抛 `http.ErrAbortHandler` 由 HTTP server 中止传输，避免把不完整文件当作成功响应；日志仅记录存储错误类型、状态和错误代码。

## 新增接口的步骤

1. 在对应 Repository 增加所需 GORM 操作；涉及结构变化时新增显式迁移。
2. 在 Service 编写权限、业务与事务逻辑，接受标准 context。
3. 在 Handler 增加命名处理方法，将其注册到正确路由组，补齐方法、路径、用途、权限的中文注释。
4. 更新 `docs/routes.md` 与相关 API 文档。
5. 执行 `make fmt`、`make check`、`make build`，按需在隔离数据库进行 HTTP 冒烟。项目禁止生成、修改或运行单元测试。

Gin 的路由与中间件用法参考[官方文档](https://gin-gonic.com/en/docs/middleware/)，GORM 事务与查询参考[事务文档](https://gorm.io/docs/transactions.html)和[查询文档](https://gorm.io/docs/query.html)。
