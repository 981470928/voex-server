import type { Request } from "express";
import type { PoolConnection, RowDataPacket } from "mysql2/promise";
import { currentUser } from "../auth/session";
import { HttpError } from "../http";
import {
  requireProjectPermission,
  resolveProjectPermissions,
} from "../permissions/project";
import { selectedTeam } from "../teams/service";
import {
  DEFAULT_PROJECT_NAME,
  ensureRoot,
  insertFolder,
  insertProject,
} from "./store";
import {
  documentDto,
  folderDto,
  projectDto,
  type DocumentRow,
  type FolderRow,
  type Project,
  type ProjectPermissions,
  type ProjectRow,
} from "./types";

export const DOCUMENT_COLUMNS = `d.id,d.file_key,d.file_name,f.project_id,p.project_key,f.folder_key,
 d.created_at,d.updated_at,d.creator_id,d.revision,t.team_key,u.name AS creator_name,u.avator AS creator_avator`;
export const DOCUMENT_FROM = `FROM documents d JOIN folders f ON f.id=d.folder_id
 JOIN projects p ON p.id=f.project_id JOIN teams t ON t.id=p.team_id LEFT JOIN users u ON u.id=d.creator_id`;
export async function findProject(
  c: PoolConnection,
  key: string,
): Promise<ProjectRow> {
  const [rows] = await c.execute<ProjectRow[]>(
    "SELECT p.*,t.team_key FROM projects p JOIN teams t ON t.id=p.team_id WHERE p.project_key=?",
    [key],
  );
  if (!rows[0]) throw new HttpError(404, "项目不存在");
  return rows[0];
}
export async function findFolder(
  c: PoolConnection,
  key: string,
): Promise<FolderRow> {
  const [rows] = await c.execute<FolderRow[]>(
    "SELECT * FROM folders WHERE folder_key=?",
    [key],
  );
  if (!rows[0]) throw new HttpError(404, "文件夹不存在");
  return rows[0];
}
export function projectPermission(
  req: Request,
  project: Pick<ProjectRow, "project_key">,
  action: keyof ProjectPermissions,
  c: PoolConnection,
) {
  return requireProjectPermission(
    req,
    { kind: "project", project_key: project.project_key },
    action,
    c,
  );
}
export async function folderProject(
  c: PoolConnection,
  folder: FolderRow,
): Promise<ProjectRow> {
  const [rows] = await c.execute<ProjectRow[]>(
    "SELECT p.*,t.team_key FROM projects p JOIN teams t ON t.id=p.team_id WHERE p.id=?",
    [folder.project_id],
  );
  if (!rows[0]) throw new HttpError(404, "项目不存在");
  return rows[0];
}
export async function listProjects(
  c: PoolConnection,
  req: Request,
  teamKey?: string,
): Promise<Project[]> {
  const team = await selectedTeam(c, req, teamKey);
  const [rows] = await c.execute<ProjectRow[]>(
    "SELECT p.*,t.team_key FROM projects p JOIN teams t ON t.id=p.team_id WHERE p.team_id=? ORDER BY p.created_at,p.id",
    [team.id],
  );
  const result: Project[] = [];
  for (const row of rows) {
    const permissions = await projectPermissionOrNone(c, req, row);
    if (permissions.read) result.push(projectDto(row, permissions));
  }
  return result;
}
async function projectPermissionOrNone(
  c: PoolConnection,
  req: Request,
  project: ProjectRow,
) {
  return resolveProjectPermissions(
    req,
    { kind: "project", project_key: project.project_key },
    c,
  );
}
export async function createProject(
  c: PoolConnection,
  req: Request,
  name: string,
  teamKey?: string,
): Promise<Project> {
  const team = await selectedTeam(c, req, teamKey);
  const project = await insertProject(c, name, currentUser(req).id, team.id);
  await ensureRoot(c, project.id);
  return projectDto(project, await projectPermission(req, project, "write", c));
}
export async function initializeWorkspace(
  c: PoolConnection,
  req: Request,
  teamKey?: string,
): Promise<Project[]> {
  return listProjects(c, req, teamKey);
}
export async function projectTree(
  c: PoolConnection,
  req: Request,
  key: string,
) {
  const p = await findProject(c, key),
    permissions = await projectPermission(req, p, "read", c);
  const [folders] = await c.execute<FolderRow[]>(
    "SELECT * FROM folders WHERE project_id=? ORDER BY created_at,id",
    [p.id],
  );
  const [documents] = await c.execute<DocumentRow[]>(
    `SELECT ${DOCUMENT_COLUMNS} ${DOCUMENT_FROM} WHERE f.project_id=? ORDER BY d.created_at,d.id`,
    [p.id],
  );
  return {
    project: projectDto(p, permissions),
    folders: folders.map((f) => folderDto(f, p.project_key, null)),
    documents: documents.map(documentDto),
  };
}
export async function renameProject(
  c: PoolConnection,
  req: Request,
  key: string,
  name: string,
) {
  const p = await findProject(c, key);
  await projectPermission(req, p, "write", c);
  await c.execute("UPDATE projects SET name=? WHERE id=?", [name, p.id]);
}
export async function deleteProject(
  c: PoolConnection,
  req: Request,
  key: string,
) {
  const p = await findProject(c, key);
  await projectPermission(req, p, "write", c);
  const [folders] = await c.execute<RowDataPacket[]>(
    "SELECT id FROM folders WHERE project_id=? LIMIT 1",
    [p.id],
  );
  if (folders.length) throw new HttpError(409, "项目中仍有文件夹，请先清空");
  await c.execute("DELETE FROM projects WHERE id=?", [p.id]);
}
export async function createFolder(
  c: PoolConnection,
  req: Request,
  projectKey: string,
  parentKey: string | null,
  name: string,
) {
  const p = await findProject(c, projectKey);
  await projectPermission(req, p, "write", c);
  if (parentKey !== null)
    throw new HttpError(400, "项目仅允许一层文件夹，文件夹下只能创建文件");
  return folderDto(
    await insertFolder(c, p.id, null, name),
    p.project_key,
    null,
  );
}
export async function renameFolder(
  c: PoolConnection,
  req: Request,
  key: string,
  name: string,
) {
  const f = await findFolder(c, key);
  await projectPermission(req, await folderProject(c, f), "write", c);
  await c.execute("UPDATE folders SET name=? WHERE id=?", [name, f.id]);
}
export async function deleteFolder(
  c: PoolConnection,
  req: Request,
  key: string,
) {
  const f = await findFolder(c, key);
  await projectPermission(req, await folderProject(c, f), "write", c);
  const [docs] = await c.execute<RowDataPacket[]>(
    "SELECT id FROM documents WHERE folder_id=? LIMIT 1",
    [f.id],
  );
  if (docs.length) throw new HttpError(409, "文件夹中仍有文件，请先清空");
  await c.execute("DELETE FROM folders WHERE id=?", [f.id]);
}
export async function documentLocation(
  c: PoolConnection,
  req: Request,
  projectKey: string | undefined,
  folderKey: string | undefined,
  teamKey?: string,
) {
  let project: ProjectRow, folder: FolderRow;
  if (folderKey) {
    folder = await findFolder(c, folderKey);
    project = await folderProject(c, folder);
    if (projectKey && projectKey !== project.project_key)
      throw new HttpError(400, "文件夹不属于指定项目");
    await projectPermission(req, project, "write", c);
  } else {
    if (projectKey) project = await findProject(c, projectKey);
    else {
      const team = await selectedTeam(c, req, teamKey);
      const visible = await listProjects(c, req, team.team_key);
      project = visible[0]
        ? await findProject(c, visible[0].project_key)
        : await insertProject(
            c,
            DEFAULT_PROJECT_NAME,
            currentUser(req).id,
            team.id,
          );
    }
    await projectPermission(req, project, "write", c);
    folder = await ensureRoot(c, project.id);
  }
  if (teamKey && teamKey !== project.team_key)
    throw new HttpError(400, "项目不属于指定团队");
  return { project, folder, folder_path: [folder.name] };
}
export async function findDocument(
  c: PoolConnection,
  req: Request,
  key: string,
  action: keyof ProjectPermissions,
  content = false,
): Promise<DocumentRow> {
  const [rows] = await c.execute<DocumentRow[]>(
    `SELECT ${DOCUMENT_COLUMNS}${content ? ",d.file_content" : ""} ${DOCUMENT_FROM} WHERE d.file_key=?`,
    [key],
  );
  if (!rows[0]) throw new HttpError(404, "文档不存在");
  await projectPermission(req, rows[0], action, c);
  return rows[0];
}
export async function projectPrivileges(
  c: PoolConnection,
  req: Request,
  key: string,
) {
  const p = await findProject(c, key);
  await projectPermission(req, p, "manage", c);
  const [members] = await c.execute<RowDataPacket[]>(
    "SELECT user_id FROM project_members WHERE project_id=? ORDER BY user_id",
    [p.id],
  );
  const privileges =
    typeof p.privileges === "string" ? JSON.parse(p.privileges) : p.privileges;
  return { ...privileges, user_ids: members.map((row) => row.user_id) };
}
export async function setProjectPrivileges(
  c: PoolConnection,
  req: Request,
  key: string,
  mode: unknown,
  ids: unknown,
) {
  const p = await findProject(c, key);
  await projectPermission(req, p, "manage", c);
  if (mode !== "inherit" && mode !== "restricted")
    throw new HttpError(400, "请选择继承团队或指定成员");
  if (
    !Array.isArray(ids) ||
    ids.length > 1000 ||
    ids.some((id) => typeof id !== "string" || id.length > 64)
  )
    throw new HttpError(400, "项目成员列表不合法");
  const selected = [...new Set(ids)] as string[];
  if (selected.length) {
    const [teamMembers] = await c.execute<RowDataPacket[]>(
      "SELECT user_id FROM team_members WHERE team_id=?",
      [p.team_id],
    );
    const allowed = new Set(teamMembers.map((m) => m.user_id));
    if (selected.some((id) => !allowed.has(id)))
      throw new HttpError(400, "只能授权给当前团队成员");
  }
  await c.execute("UPDATE projects SET privileges=? WHERE id=?", [
    JSON.stringify({ mode }),
    p.id,
  ]);
  await c.execute("DELETE FROM project_members WHERE project_id=?", [p.id]);
  if (mode === "restricted")
    for (const id of selected)
      await c.execute(
        "INSERT INTO project_members (project_id,user_id) VALUES (?,?)",
        [p.id, id],
      );
  const updated = await findProject(c, key);
  return {
    ...projectDto(updated, await projectPermission(req, updated, "read", c)),
    privileges: { mode, user_ids: mode === "restricted" ? selected : [] },
  };
}
