import type { Pool, PoolConnection, RowDataPacket } from "mysql2/promise";
import { ensurePersonalTeam } from "../teams/store";

async function exists(c: PoolConnection, table: string, column: string) {
  const [rows] = await c.execute<RowDataPacket[]>(
    "SELECT COLUMN_NAME FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME=?",
    [table, column],
  );
  return rows.length > 0;
}
export async function migrateTeams(pool: Pool): Promise<void> {
  const c = await pool.getConnection();
  let locked = false;
  try {
    const [lock] = await c.query<RowDataPacket[]>(
      "SELECT GET_LOCK('voex-teams-migration',30) AS acquired",
    );
    if (Number(lock[0].acquired) !== 1)
      throw new Error("Team migration lock failed");
    locked = true;
    const [done] = await c.execute<RowDataPacket[]>(
      "SELECT version FROM schema_migrations WHERE version='003-teams'",
    );
    if (done.length) return;
    const [nested] = await c.query<RowDataPacket[]>(
      "SELECT id FROM folders WHERE parent_id IS NOT NULL LIMIT 1",
    );
    if (nested.length)
      throw new Error("Nested folders must be explicitly migrated first");
    await c.query(`CREATE TABLE IF NOT EXISTS teams (
      id INT AUTO_INCREMENT PRIMARY KEY,
      team_key VARCHAR(64) COLLATE utf8mb4_bin NOT NULL UNIQUE,
      team_code CHAR(10) CHARACTER SET ascii COLLATE ascii_bin NOT NULL UNIQUE,
      name VARCHAR(128) NOT NULL,
      kind ENUM('personal','standard') NOT NULL,
      owner_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
      personal_owner_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL UNIQUE,
      privileges JSON NOT NULL,
      created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
      updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
      FOREIGN KEY (owner_id) REFERENCES users(id),
      FOREIGN KEY (personal_owner_id) REFERENCES users(id),
      CHECK ((kind='personal' AND personal_owner_id IS NOT NULL AND personal_owner_id=owner_id) OR (kind='standard' AND personal_owner_id IS NULL))
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`);
    await c.query(`CREATE TABLE IF NOT EXISTS team_members (
      team_id INT NOT NULL,
      user_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
      role ENUM('admin','member') NOT NULL DEFAULT 'member',
      joined_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
      PRIMARY KEY (team_id,user_id),
      FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
      FOREIGN KEY (user_id) REFERENCES users(id)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`);
    if (!(await exists(c, "projects", "team_id")))
      await c.query(
        "ALTER TABLE projects ADD COLUMN team_id INT NULL, ADD INDEX idx_projects_team (team_id), ADD CONSTRAINT fk_projects_team FOREIGN KEY (team_id) REFERENCES teams(id)",
      );
    for (const table of ["projects", "folders", "documents", "files"]) {
      if (!(await exists(c, table, "privileges")))
        await c.query(`ALTER TABLE ${table} ADD COLUMN privileges JSON NULL`);
      await c.query(
        `UPDATE ${table} SET privileges=JSON_OBJECT('mode','inherit'), ${table === "files" ? "created_at=created_at" : "updated_at=updated_at"} WHERE privileges IS NULL`,
      );
      await c.query(
        `ALTER TABLE ${table} MODIFY COLUMN privileges JSON NOT NULL`,
      );
    }
    if (!(await exists(c, "documents", "revision")))
      await c.query(
        "ALTER TABLE documents ADD COLUMN revision INT UNSIGNED NOT NULL DEFAULT 1",
      );
    await c.query(`CREATE TABLE IF NOT EXISTS project_members (
      project_id INT NOT NULL,
      user_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
      PRIMARY KEY (project_id,user_id),
      FOREIGN KEY (project_id) REFERENCES projects(id) ON DELETE CASCADE,
      FOREIGN KEY (user_id) REFERENCES users(id)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`);
    await c.query(`CREATE TABLE IF NOT EXISTS team_invites (
      id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
      team_id INT NOT NULL,
      token_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL UNIQUE,
      created_by CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
      created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
      revoked_at DATETIME NULL,
      FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
      FOREIGN KEY (created_by) REFERENCES users(id)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`);
    await c.query(`CREATE TABLE IF NOT EXISTS team_join_requests (
      id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
      team_id INT NOT NULL,
      user_id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
      message VARCHAR(1000) NOT NULL,
      status ENUM('pending','approved','rejected') NOT NULL DEFAULT 'pending',
      reviewed_by CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NULL,
      created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
      reviewed_at DATETIME NULL,
      UNIQUE KEY uq_team_request (team_id,user_id),
      FOREIGN KEY (team_id) REFERENCES teams(id) ON DELETE CASCADE,
      FOREIGN KEY (user_id) REFERENCES users(id),
      FOREIGN KEY (reviewed_by) REFERENCES users(id)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`);
    await c.query(`CREATE TABLE IF NOT EXISTS file_shares (
      id CHAR(36) CHARACTER SET ascii COLLATE ascii_bin PRIMARY KEY,
      document_id INT NOT NULL,
      token_hash CHAR(64) CHARACTER SET ascii COLLATE ascii_bin NOT NULL UNIQUE,
      permission ENUM('read','edit') NOT NULL,
      created_by CHAR(36) CHARACTER SET ascii COLLATE ascii_bin NOT NULL,
      created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
      revoked_at DATETIME NULL,
      FOREIGN KEY (document_id) REFERENCES documents(id) ON DELETE CASCADE,
      FOREIGN KEY (created_by) REFERENCES users(id)
    ) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4`);
    await c.beginTransaction();
    await c.query("SELECT id FROM workspace_state WHERE id=1 FOR UPDATE");
    const [users] = await c.query<RowDataPacket[]>("SELECT id,name FROM users");
    if (users.length) {
      for (const user of users) {
        const teamId = await ensurePersonalTeam(c, user.id, user.name);
        await c.execute(
          "UPDATE projects SET team_id=?, updated_at=updated_at WHERE creator_id=? AND team_id IS NULL",
          [teamId, user.id],
        );
      }
      const [unassigned] = await c.query<RowDataPacket[]>(
        "SELECT id FROM projects WHERE team_id IS NULL LIMIT 1",
      );
      if (unassigned.length)
        throw new Error(
          "Projects without creator require an explicit team assignment",
        );
      await c.commit();
      await c.query("ALTER TABLE projects MODIFY COLUMN team_id INT NOT NULL");
    } else {
      await c.query("DELETE FROM documents WHERE folder_id IN (SELECT id FROM folders)");
      await c.query("DELETE FROM folders");
      await c.query("DELETE FROM projects");
      await c.commit();
    }
    const [constraints] = await c.query<RowDataPacket[]>(
      "SELECT CONSTRAINT_NAME FROM information_schema.TABLE_CONSTRAINTS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME='folders' AND CONSTRAINT_NAME='chk_folders_flat'",
    );
    if (!constraints.length)
      await c.query(
        "ALTER TABLE folders ADD CONSTRAINT chk_folders_flat CHECK (parent_id IS NULL)",
      );
    await c.execute(
      "INSERT INTO schema_migrations (version) VALUES ('003-teams')",
    );
  } catch (error) {
    await c.rollback();
    throw error;
  } finally {
    if (locked) await c.query("SELECT RELEASE_LOCK('voex-teams-migration')");
    c.release();
  }
}
