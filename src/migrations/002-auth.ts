import type { Pool, RowDataPacket } from "mysql2/promise";

export async function migrateAuth(pool: Pool): Promise<void> {
  const c = await pool.getConnection();
  let locked = false;
  try {
    const [locks] = await c.query<RowDataPacket[]>("SELECT GET_LOCK('voex-auth-migration', 30) AS acquired");
    if (Number(locks[0].acquired) !== 1) throw new Error("认证迁移锁获取失败");
    locked = true;
    await c.query(`CREATE TABLE IF NOT EXISTS users (
      id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
      account VARBINARY(256) NOT NULL UNIQUE,
      password_hash VARCHAR(255) NOT NULL,
      name VARCHAR(64) NOT NULL,
      avator VARCHAR(255) NOT NULL DEFAULT '',
      email VARCHAR(254) NOT NULL DEFAULT '',
      phone VARCHAR(32) NOT NULL DEFAULT '',
      created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
      updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`);
    await c.query(`CREATE TABLE IF NOT EXISTS auth_sessions (
      id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
      user_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
      refresh_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL UNIQUE,
      expires_at DATETIME NOT NULL,
      created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
      INDEX idx_session_expiry (expires_at),
      FOREIGN KEY (user_id) REFERENCES users(id)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`);
    for (const table of ['projects', 'documents', 'files']) {
      const [columns] = await c.execute<RowDataPacket[]>(
        'SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = ? AND COLUMN_NAME = ?',
        [table, 'creator_id'],
      );
      if (!columns.length) await c.query(`ALTER TABLE ${table}
        ADD COLUMN creator_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL,
        ADD INDEX idx_${table}_creator (creator_id),
        ADD CONSTRAINT fk_${table}_creator FOREIGN KEY (creator_id) REFERENCES users(id)`);
    }
    await c.query(`CREATE TABLE IF NOT EXISTS assets (
      id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
      creator_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
      directory VARCHAR(255) NOT NULL,
      storage_name VARCHAR(64) NOT NULL,
      name VARCHAR(255) NOT NULL,
      mime VARCHAR(128) NOT NULL,
      size BIGINT UNSIGNED NOT NULL,
      created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
      INDEX idx_asset_creator (creator_id),
      FOREIGN KEY (creator_id) REFERENCES users(id)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`);
    await c.execute('INSERT IGNORE INTO schema_migrations (version) VALUES (?)', ['002-auth']);
  } finally {
    if (locked) await c.query("SELECT RELEASE_LOCK('voex-auth-migration')");
    c.release();
  }
}
