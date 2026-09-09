数据库： 127.0.0.1:3306
账号： root
密码： chenruiok9814
库名： file_server

---

## 数据库设计

### 表 1: `documents` — 文档表

```sql
CREATE TABLE IF NOT EXISTS documents (
  id          INT AUTO_INCREMENT PRIMARY KEY,
  file_key    VARCHAR(64)  NOT NULL UNIQUE,
  file_name   VARCHAR(255) NOT NULL DEFAULT 'untitled.md',
  file_content LONGTEXT,
  created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
  INDEX idx_file_key (file_key)
);
```

| 字段           | 类型                                | 说明                 |
| -------------- | ----------------------------------- | -------------------- |
| `id`           | INT, PK, AUTO_INCREMENT             | 主键                 |
| `file_key`     | VARCHAR(64), UNIQUE, INDEX          | 文档唯一标识         |
| `file_name`    | VARCHAR(255), DEFAULT 'untitled.md' | 文档名称             |
| `file_content` | LONGTEXT, NULLABLE                  | 文档内容（Markdown） |
| `created_at`   | DATETIME                            | 创建时间             |
| `updated_at`   | DATETIME                            | 更新时间             |

### 表 2: `files` — 文件资源关联表

```sql
CREATE TABLE IF NOT EXISTS files (
  id         INT AUTO_INCREMENT PRIMARY KEY,
  file_key   VARCHAR(64)  NOT NULL,
  hash       VARCHAR(128) NOT NULL,
  name       VARCHAR(255) NOT NULL,
  mime       VARCHAR(128) NOT NULL,
  size       BIGINT       NOT NULL DEFAULT 0,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  INDEX idx_file_key (file_key),
  INDEX idx_hash (hash)
);
```

| 字段         | 类型                    | 说明                    |
| ------------ | ----------------------- | ----------------------- |
| `id`         | INT, PK, AUTO_INCREMENT | 主键                    |
| `file_key`   | VARCHAR(64), INDEX      | 关联文档的 file_key     |
| `hash`       | VARCHAR(128), INDEX     | 文件内容 SHA-256 哈希值 |
| `name`       | VARCHAR(255)            | 原始文件名              |
| `mime`       | VARCHAR(128)            | MIME 类型               |
| `size`       | BIGINT                  | 文件大小（字节）        |
| `created_at` | DATETIME                | 上传时间                |

---

## 文件去重策略

- 文件以 hash 命名存储在 `/home/static/{hash}`
- 相同内容的文件只存一份物理文件，`files` 表中每条上传记录都保留
- 删除文档时：先删除 `files` 表中关联记录，再检查 hash 是否仍被其他记录引用
  - 若无引用 → 删除物理文件
  - 若仍有引用 → 保留物理文件
