import type { Pool, PoolConnection, RowDataPacket } from "mysql2/promise";
import {
  DEFAULT_PROJECT_NAME,
  ensureRoot,
  insertProject,
  lockWorkspace,
  orderedProjects,
} from "../workspace/store";

const VERSION = "001-workspace";
const LOCK_NAME = "CONCAT('vode:', LEFT(SHA2(DATABASE(), 256), 48))";

async function exists(
  connection: PoolConnection,
  query: string,
  values: string[],
): Promise<boolean> {
  const [rows] = await connection.execute<RowDataPacket[]>(query, values);
  return rows.length > 0;
}

/**
 * MySQL DDL 会隐式提交。以连接级 advisory lock 保护整个 migration，
 * 各 DDL 步骤检查 information_schema，数据回填单独事务，允许失败后重入。
 * 只在正式启动 initDatabase 时调用；编译不会执行或连接数据库。
 */
export async function migrateWorkspace(pool: Pool): Promise<void> {
  const connection = await pool.getConnection();
  let locked = false;
  try {
    const [locks] = await connection.query<RowDataPacket[]>(
      `SELECT GET_LOCK(${LOCK_NAME}, 30) AS acquired`,
    );
    if (Number(locks[0].acquired) !== 1) {
      throw new Error("无法取得 schema migration 锁，请稍后重新启动");
    }
    locked = true;

    await connection.execute(`
      CREATE TABLE IF NOT EXISTS schema_migrations (
        version VARCHAR(64) NOT NULL PRIMARY KEY,
        applied_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
      ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
    `);
    if (await exists(connection,
      "SELECT version FROM schema_migrations WHERE version = ?", [VERSION])) {
      return;
    }

    const [engines] = await connection.execute<RowDataPacket[]>(
      `SELECT ENGINE FROM information_schema.TABLES
       WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME IN ('documents', 'files')`,
    );
    if (engines.length !== 2 || engines.some((row) => row.ENGINE !== "InnoDB")) {
      throw new Error("迁移要求 documents 和 files 使用 InnoDB；请先在维护窗口确认存储引擎");
    }

    await connection.execute(`
      CREATE TABLE IF NOT EXISTS workspace_state (
        id TINYINT NOT NULL PRIMARY KEY
      ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
    `);
    await connection.execute("INSERT IGNORE INTO workspace_state (id) VALUES (1)");

    await connection.execute(`
      CREATE TABLE IF NOT EXISTS projects (
        id INT AUTO_INCREMENT PRIMARY KEY,
        project_key VARCHAR(64) COLLATE utf8mb4_bin NOT NULL UNIQUE,
        name VARCHAR(255) NOT NULL,
        created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
        INDEX idx_projects_order (created_at, id)
      ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
    `);

    await connection.execute(`
      CREATE TABLE IF NOT EXISTS folders (
        id INT AUTO_INCREMENT PRIMARY KEY,
        folder_key VARCHAR(64) COLLATE utf8mb4_bin NOT NULL UNIQUE,
        project_id INT NOT NULL,
        parent_id INT NULL,
        name VARCHAR(255) NOT NULL,
        created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
        UNIQUE KEY uq_folders_project_id (project_id, id),
        INDEX idx_folders_parent (project_id, parent_id, created_at, id),
        INDEX idx_folders_order (project_id, created_at, id),
        CONSTRAINT fk_folders_project FOREIGN KEY (project_id)
          REFERENCES projects (id) ON DELETE RESTRICT ON UPDATE RESTRICT,
        CONSTRAINT fk_folders_parent FOREIGN KEY (project_id, parent_id)
          REFERENCES folders (project_id, id) ON DELETE RESTRICT ON UPDATE RESTRICT
      ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
    `);

    if (!await exists(connection,
      `SELECT COLUMN_NAME FROM information_schema.COLUMNS
       WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'documents' AND COLUMN_NAME = ?`,
      ["folder_id"])) {
      await connection.execute("ALTER TABLE documents ADD COLUMN folder_id INT NULL");
    }

    await connection.beginTransaction();
    await lockWorkspace(connection);
    const [unassigned] = await connection.execute<RowDataPacket[]>(
      "SELECT id FROM documents WHERE folder_id IS NULL LIMIT 1",
    );
    if (unassigned.length > 0) {
      const project = (await orderedProjects(connection))[0] ??
        await insertProject(connection, DEFAULT_PROJECT_NAME);
      const folder = await ensureRoot(connection, project.id, false);
      // 显式保留 updated_at，防止回填归属触发旧表的 ON UPDATE。
      await connection.execute(
        "UPDATE documents SET folder_id = ?, updated_at = updated_at WHERE folder_id IS NULL",
        [folder.id],
      );
    }
    await connection.commit();

    const [columns] = await connection.execute<RowDataPacket[]>(
      `SELECT IS_NULLABLE FROM information_schema.COLUMNS
       WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'documents' AND COLUMN_NAME = 'folder_id'`,
    );
    if (columns[0].IS_NULLABLE === "YES") {
      await connection.execute("ALTER TABLE documents MODIFY COLUMN folder_id INT NOT NULL");
    }

    if (!await exists(connection,
      `SELECT INDEX_NAME FROM information_schema.STATISTICS
       WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'documents' AND INDEX_NAME = ?`,
      ["idx_documents_folder"])) {
      await connection.execute(
        "ALTER TABLE documents ADD INDEX idx_documents_folder (folder_id, created_at, id)",
      );
    }
    if (!await exists(connection,
      `SELECT CONSTRAINT_NAME FROM information_schema.TABLE_CONSTRAINTS
       WHERE CONSTRAINT_SCHEMA = DATABASE() AND TABLE_NAME = 'documents'
         AND CONSTRAINT_TYPE = 'FOREIGN KEY' AND CONSTRAINT_NAME = ?`,
      ["fk_documents_folder"])) {
      await connection.execute(`
        ALTER TABLE documents ADD CONSTRAINT fk_documents_folder
        FOREIGN KEY (folder_id) REFERENCES folders (id)
        ON DELETE RESTRICT ON UPDATE RESTRICT
      `);
    }
    await connection.execute("INSERT INTO schema_migrations (version) VALUES (?)", [VERSION]);
  } catch (error) {
    try {
      await connection.rollback();
    } catch {
      connection.destroy();
    }
    throw error;
  } finally {
    if (locked) {
      try {
        await connection.query(`SELECT RELEASE_LOCK(${LOCK_NAME})`);
      } catch {
        // 不把仍持有连接级锁的连接放回池中。
        connection.destroy();
      }
    }
    connection.release();
  }
}
