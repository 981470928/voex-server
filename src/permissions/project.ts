import type { Request } from "express";
import type { Pool, PoolConnection, RowDataPacket } from "mysql2/promise";
import { getPool } from "../db";
import { HttpError } from "../http";
import { currentUser } from "../auth/session";
import { policy } from "../teams/store";
import type { ProjectPermissions } from "../workspace/types";
export type ProjectPermissionScope =
  { kind: "workspace" } | { kind: "project"; project_key: string };
export async function resolveProjectPermissions(
  request: Request,
  scope: ProjectPermissionScope,
  c: Pool | PoolConnection = getPool(),
): Promise<ProjectPermissions> {
  const user = currentUser(request);
  if (scope.kind === "workspace")
    return { read: true, write: true, manage: true, share: true };
  const [rows] = await c.execute<RowDataPacket[]>(
    `SELECT t.owner_id,m.user_id,m.role,p.privileges,
  EXISTS(SELECT 1 FROM project_members pm WHERE pm.project_id=p.id AND pm.user_id=?) AS explicit_member
  FROM projects p JOIN teams t ON t.id=p.team_id
  LEFT JOIN team_members m ON m.team_id=t.id AND m.user_id=? WHERE p.project_key=?`,
    [user.id, user.id, scope.project_key],
  );
  const row = rows[0];
  const manager =
    !!row?.user_id && (row.owner_id === user.id || row.role === "admin");
  const edit =
    !!row?.user_id &&
    (manager ||
      policy(row.privileges).mode === "inherit" ||
      !!row.explicit_member);
  return { read: edit, write: edit, manage: manager, share: edit };
}
export async function requireProjectPermission(
  request: Request,
  scope: ProjectPermissionScope,
  action: keyof ProjectPermissions,
  c: Pool | PoolConnection = getPool(),
): Promise<ProjectPermissions> {
  const permissions = await resolveProjectPermissions(request, scope, c);
  if (!permissions[action])
    throw new HttpError(404, "项目不存在或没有访问权限");
  return permissions;
}
