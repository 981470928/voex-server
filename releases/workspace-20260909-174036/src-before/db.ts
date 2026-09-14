import mysql from "mysql2/promise";

const DB_CONFIG = {
  host: "127.0.0.1",
  port: 3306,
  user: "root",
  password: "chenruiok9814",
  database: "file_server",
};

let pool: mysql.Pool | null = null;

export async function initDatabase(): Promise<mysql.Pool> {
  const conn = await mysql.createConnection({
    host: DB_CONFIG.host,
    port: DB_CONFIG.port,
    user: DB_CONFIG.user,
    password: DB_CONFIG.password,
  });

  await conn.execute("CREATE DATABASE IF NOT EXISTS `file_server`");
  await conn.end();

  pool = mysql.createPool({
    ...DB_CONFIG,
    waitForConnections: true,
    connectionLimit: 10,
    queueLimit: 0,
  });

  await pool.execute(`
    CREATE TABLE IF NOT EXISTS documents (
      id           INT AUTO_INCREMENT PRIMARY KEY,
      file_key     VARCHAR(64)  NOT NULL UNIQUE,
      file_name    VARCHAR(255) NOT NULL DEFAULT 'untitled.md',
      file_content LONGTEXT,
      created_at   DATETIME DEFAULT CURRENT_TIMESTAMP,
      updated_at   DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
      INDEX idx_file_key (file_key)
    )
  `);

  await pool.execute(`
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
    )
  `);

  console.log("[DB] Database and tables initialized");
  return pool;
}

export function getPool(): mysql.Pool {
  if (!pool)
    throw new Error("Database not initialized. Call initDatabase() first.");
  return pool;
}
