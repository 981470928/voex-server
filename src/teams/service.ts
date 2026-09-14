import type { Request } from "express";
import type { PoolConnection, RowDataPacket } from "mysql2/promise";
import { currentUser } from "../auth/session";
import { HttpError, nameValue } from "../http";
import { timestamp } from "../workspace/types";
import { insertTeam, ensurePersonalTeam } from "./store";

export async function teamRow(
  c: PoolConnection,
  req: Request,
  key: string,
  manager = false,
): Promise<RowDataPacket> {
  const user = currentUser(req);
  const [rows] = await c.execute<RowDataPacket[]>(
    `SELECT t.*,m.user_id AS member_id,
  CASE WHEN t.owner_id=m.user_id THEN 'owner' ELSE m.role END AS role
  FROM teams t JOIN team_members m ON m.team_id=t.id AND m.user_id=? WHERE t.team_key=?`,
    [user.id, key],
  );
  const team = rows[0];
  if (!team) throw new HttpError(404, "团队不存在或尚未加入");
  if (manager && team.role === "member")
    throw new HttpError(403, "只有团队所有者和管理员可以操作");
  return team;
}
export function teamDto(row: RowDataPacket) {
  const manager = row.role === "owner" || row.role === "admin";
  return {
    team_key: row.team_key,
    team_code: row.team_code,
    name: row.name,
    kind: row.kind,
    role: row.role,
    owner_id: row.owner_id,
    member_count: Number(row.member_count ?? 1),
    created_at: timestamp(row.created_at),
    privileges: { mode: "members" },
    permissions: {
      read: true,
      write: true,
      manage: manager,
      transfer: row.role === "owner" && row.kind === "standard",
      delete: row.role === "owner" && row.kind === "standard",
    },
  };
}
export async function getTeam(c: PoolConnection, req: Request, key: string) {
  const row = await teamRow(c, req, key);
  const [count] = await c.execute<RowDataPacket[]>(
    "SELECT COUNT(*) AS count FROM team_members WHERE team_id=?",
    [row.id],
  );
  return teamDto({ ...row, member_count: count[0].count } as RowDataPacket);
}
export async function listTeams(c: PoolConnection, req: Request) {
  const [rows] = await c.execute<RowDataPacket[]>(
    `SELECT t.*,CASE WHEN t.owner_id=m.user_id THEN 'owner' ELSE m.role END AS role,
  (SELECT COUNT(*) FROM team_members tm WHERE tm.team_id=t.id) AS member_count
  FROM teams t JOIN team_members m ON m.team_id=t.id WHERE m.user_id=? ORDER BY (t.personal_owner_id=?) DESC,t.created_at,t.id`,
    [currentUser(req).id, currentUser(req).id],
  );
  return rows.map(teamDto);
}
export async function createTeam(
  c: PoolConnection,
  req: Request,
  name: unknown,
) {
  const title = nameValue(name, "团队名称");
  if (Array.from(title).length > 128)
    throw new HttpError(400, "团队名称最多128个字符");
  const id = await insertTeam(c, currentUser(req).id, title, false);
  const [rows] = await c.execute<RowDataPacket[]>(
    "SELECT team_key FROM teams WHERE id=?",
    [id],
  );
  return getTeam(c, req, rows[0].team_key);
}
export async function selectedTeam(
  c: PoolConnection,
  req: Request,
  key?: string,
) {
  if (key) return teamRow(c, req, key);
  const [rows] = await c.execute<RowDataPacket[]>(
    "SELECT team_key FROM teams WHERE personal_owner_id=?",
    [currentUser(req).id],
  );
  if (!rows[0]) throw new HttpError(409, "个人团队尚未初始化");
  return teamRow(c, req, rows[0].team_key);
}
export async function members(c: PoolConnection, req: Request, key: string) {
  const team = await teamRow(c, req, key);
  const [rows] = await c.execute<RowDataPacket[]>(
    `SELECT u.id AS user_id,u.name,u.avator,CASE WHEN u.id=? THEN 'owner' ELSE m.role END AS role,m.joined_at
  FROM team_members m JOIN users u ON u.id=m.user_id WHERE m.team_id=? ORDER BY (u.id=?) DESC,(m.role='admin') DESC,m.joined_at,u.id`,
    [team.owner_id, team.id, team.owner_id],
  );
  return rows.map((row) => ({ ...row, joined_at: timestamp(row.joined_at) }));
}
