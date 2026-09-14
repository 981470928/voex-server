import { randomInt } from "node:crypto";
import type { PoolConnection, RowDataPacket } from "mysql2/promise";
import { publicKey } from "../workspace/store";

export async function insertTeam(
  c: PoolConnection,
  userId: string,
  name: string,
  personal: boolean,
): Promise<number> {
  for (let attempt = 0; attempt < 8; attempt++) {
    try {
      const [result]: any = await c.execute(
        `INSERT INTO teams (team_key, team_code, name, kind, owner_id, personal_owner_id, privileges)
         VALUES (?, ?, ?, ?, ?, ?, JSON_OBJECT('mode','members'))`,
        [
          publicKey(),
          `${randomInt(1, 10)}${randomInt(0, 1000000000).toString().padStart(9, "0")}`,
          name,
          personal ? "personal" : "standard",
          userId,
          personal ? userId : null,
        ],
      );
      const id = Number(result.insertId);
      await c.execute(
        "INSERT INTO team_members (team_id, user_id, role) VALUES (?, ?, 'member')",
        [id, userId],
      );
      return id;
    } catch (error) {
      if ((error as { code?: string }).code !== "ER_DUP_ENTRY") throw error;
      if (personal) {
        const [rows] = await c.execute<RowDataPacket[]>(
          "SELECT id FROM teams WHERE personal_owner_id = ?",
          [userId],
        );
        if (rows[0]) return Number(rows[0].id);
      }
    }
  }
  throw new Error("Unable to allocate unique team identifier");
}
export async function ensurePersonalTeam(
  c: PoolConnection,
  userId: string,
  name: string,
): Promise<number> {
  const [rows] = await c.execute<RowDataPacket[]>(
    "SELECT id FROM teams WHERE personal_owner_id = ?",
    [userId],
  );
  return rows[0]
    ? Number(rows[0].id)
    : insertTeam(c, userId, `${name}的个人团队`, true);
}
export function policy(value: unknown): { mode: string } {
  const parsed = typeof value === "string" ? JSON.parse(value) : value;
  if (!parsed || typeof parsed !== "object" || !("mode" in parsed))
    return { mode: "restricted" };
  return parsed as { mode: string };
}
