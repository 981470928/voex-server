import type {
  PoolConnection,
  ResultSetHeader,
  RowDataPacket,
} from "mysql2/promise";
import { v4 as uuidv4 } from "uuid";
import type { FolderRow, ProjectRow } from "./types";

export const DEFAULT_PROJECT_NAME = "默认项目";
export const DEFAULT_FOLDER_NAME = "默认文件夹";

export function publicKey(): string {
  return uuidv4().replace(/-/g, "").slice(0, 16);
}

/** 所有结构写入必须先在同一事务中取得此行锁；跨进程共享，提交/回滚时释放。 */
export async function lockWorkspace(connection: PoolConnection): Promise<void> {
  const [rows] = await connection.execute<RowDataPacket[]>(
    "SELECT id FROM workspace_state WHERE id = 1 FOR UPDATE",
  );
  if (rows.length !== 1)
    throw new Error("工作区锁记录缺失，请先完成 schema migration");
}

export async function orderedProjects(
  connection: PoolConnection,
  creatorId?: string,
): Promise<ProjectRow[]> {
  const [rows] = await connection.execute<ProjectRow[]>(
    `SELECT id, project_key, name, created_at, updated_at FROM projects ${creatorId ? "WHERE creator_id = ?" : ""} ORDER BY created_at ASC, id ASC`,
    creatorId ? [creatorId] : [],
  );
  return rows;
}

export async function insertProject(
  connection: PoolConnection,
  name: string,
  creatorId: string | null = null,
  teamId?: number,
): Promise<ProjectRow> {
  const key = publicKey();
  const [result] = await connection.execute<ResultSetHeader>(
    teamId
      ? "INSERT INTO projects (project_key,name,creator_id,team_id,privileges) VALUES (?,?,?,?,JSON_OBJECT('mode','inherit'))"
      : "INSERT INTO projects (project_key,name) VALUES (?,?)",
    teamId ? [key, name, creatorId, teamId] : [key, name],
  );
  const [rows] = await connection.execute<ProjectRow[]>(
    teamId
      ? "SELECT p.*,t.team_key FROM projects p JOIN teams t ON t.id=p.team_id WHERE p.id=?"
      : "SELECT * FROM projects WHERE id=?",
    [result.insertId],
  );
  return rows[0];
}

export async function insertFolder(
  connection: PoolConnection,
  projectId: number,
  parentId: number | null,
  name: string,
  withPrivileges = true,
): Promise<FolderRow> {
  const [result] = await connection.execute<ResultSetHeader>(
    withPrivileges
      ? "INSERT INTO folders (folder_key,project_id,parent_id,name,privileges) VALUES (?,?,?,?,JSON_OBJECT('mode','inherit'))"
      : "INSERT INTO folders (folder_key,project_id,parent_id,name) VALUES (?,?,?,?)",
    [publicKey(), projectId, parentId, name],
  );
  const [rows] = await connection.execute<FolderRow[]>(
    "SELECT id, folder_key, project_id, parent_id, name, created_at, updated_at FROM folders WHERE id = ?",
    [result.insertId],
  );
  return rows[0];
}

export async function firstRoot(
  connection: PoolConnection,
  projectId: number,
): Promise<FolderRow | undefined> {
  const [rows] = await connection.execute<FolderRow[]>(
    `SELECT id, folder_key, project_id, parent_id, name, created_at, updated_at
     FROM folders WHERE project_id = ? AND parent_id IS NULL
     ORDER BY created_at ASC, id ASC LIMIT 1`,
    [projectId],
  );
  return rows[0];
}

/** 调用方须持有 workspace 行锁并负责权限校验；migration 在维护窗口调用。 */
export async function ensureRoot(
  connection: PoolConnection,
  projectId: number,
  withPrivileges = true,
): Promise<FolderRow> {
  return (
    (await firstRoot(connection, projectId)) ??
    insertFolder(
      connection,
      projectId,
      null,
      DEFAULT_FOLDER_NAME,
      withPrivileges,
    )
  );
}
