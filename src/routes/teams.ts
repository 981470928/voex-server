import { Router } from "express";
import type { RowDataPacket } from "mysql2/promise";
import { randomBytes, randomUUID } from "node:crypto";
import { rateLimit } from "express-rate-limit";
import {
  asyncRoute,
  bodyObject,
  HttpError,
  keyValue,
  nameValue,
} from "../http";
import { currentUser, digest } from "../auth/session";
import { workspaceTransaction } from "../workspace/transaction";
import {
  createTeam,
  getTeam,
  listTeams,
  members,
  teamRow,
} from "../teams/service";
import { timestamp } from "../workspace/types";
import { getPool } from "../db";

export const teamsRouter = Router();
export const publicTeamsRouter = Router();
const inviteLimit = rateLimit({
  windowMs: 60000,
  limit: 60,
  standardHeaders: "draft-8",
  legacyHeaders: false,
  message: { error: "操作过于频繁，请稍后重试" },
});
publicTeamsRouter.post(
  "/team-invites/inspect",
  inviteLimit,
  asyncRoute(async (req, res) => {
    const token = bodyObject(req.body).token;
    if (typeof token !== "string" || !/^[A-Za-z0-9_-]{43}$/.test(token))
      throw new HttpError(404, "邀请链接无效或已撤销");
    const [rows] = await getPool().execute<RowDataPacket[]>(
      `SELECT t.team_key,t.team_code,t.name,t.kind FROM teams t JOIN team_invites i ON i.team_id=t.id WHERE i.token_hash=? AND i.revoked_at IS NULL`,
      [digest(token)],
    );
    if (!rows[0]) throw new HttpError(404, "邀请链接无效或已撤销");
    res.json(rows[0]);
  }),
);
teamsRouter.get(
  "/teams",
  asyncRoute(async (req, res) => {
    res.json(await workspaceTransaction(false, (c) => listTeams(c, req)));
  }),
);
teamsRouter.post(
  "/teams",
  asyncRoute(async (req, res) => {
    res
      .status(201)
      .json(
        await workspaceTransaction(true, (c) =>
          createTeam(c, req, bodyObject(req.body).name),
        ),
      );
  }),
);
teamsRouter.get(
  "/teams/:key",
  asyncRoute(async (req, res) => {
    res.json(
      await workspaceTransaction(false, (c) =>
        getTeam(c, req, keyValue(req.params.key, "团队标识")),
      ),
    );
  }),
);
teamsRouter.patch(
  "/teams/:key",
  asyncRoute(async (req, res) => {
    res.json(
      await workspaceTransaction(true, async (c) => {
        const t = await teamRow(c, req, req.params.key, true);
        const name = nameValue(bodyObject(req.body).name, "团队名称");
        if (Array.from(name).length > 128)
          throw new HttpError(400, "团队名称最多128个字符");
        await c.execute("UPDATE teams SET name=? WHERE id=?", [name, t.id]);
        return getTeam(c, req, t.team_key);
      }),
    );
  }),
);
teamsRouter.delete(
  "/teams/:key",
  asyncRoute(async (req, res) => {
    await workspaceTransaction(true, async (c) => {
      const t = await teamRow(c, req, req.params.key, true);
      if (t.role !== "owner")
        throw new HttpError(403, "只有所有者可以删除团队");
      if (t.kind === "personal") throw new HttpError(403, "个人团队无法删除");
      const [projects] = await c.execute<RowDataPacket[]>(
        "SELECT id FROM projects WHERE team_id=? LIMIT 1",
        [t.id],
      );
      if (projects.length) throw new HttpError(409, "请先清空团队中的项目");
      await c.execute("DELETE FROM teams WHERE id=?", [t.id]);
    });
    res.json({ success: true });
  }),
);
teamsRouter.post(
  "/teams/:key/transfer",
  asyncRoute(async (req, res) => {
    await workspaceTransaction(true, async (c) => {
      const t = await teamRow(c, req, req.params.key, true);
      if (t.role !== "owner" || t.kind === "personal")
        throw new HttpError(403, "仅普通团队的所有者可以转让团队");
      const userId = keyValue(bodyObject(req.body).user_id, "成员标识");
      if (userId === t.owner_id) throw new HttpError(400, "请选择其他团队成员");
      const [rows] = await c.execute<RowDataPacket[]>(
        "SELECT user_id FROM team_members WHERE team_id=? AND user_id=?",
        [t.id, userId],
      );
      if (!rows.length) throw new HttpError(400, "新所有者必须是当前团队成员");
      await c.execute(
        "UPDATE team_members SET role='admin' WHERE team_id=? AND user_id=?",
        [t.id, t.owner_id],
      );
      await c.execute("UPDATE teams SET owner_id=? WHERE id=?", [userId, t.id]);
    });
    res.json({ success: true });
  }),
);
teamsRouter.get(
  "/teams/:key/members",
  asyncRoute(async (req, res) => {
    res.json(
      await workspaceTransaction(false, (c) => members(c, req, req.params.key)),
    );
  }),
);
teamsRouter.patch(
  "/teams/:key/members/:userId",
  asyncRoute(async (req, res) => {
    await workspaceTransaction(true, async (c) => {
      const t = await teamRow(c, req, req.params.key, true);
      if (t.role !== "owner")
        throw new HttpError(403, "只有所有者可以设置管理员");
      if (req.params.userId === t.owner_id)
        throw new HttpError(403, "所有者身份只能通过团队转让变更");
      const role = bodyObject(req.body).role;
      if (role !== "admin" && role !== "member")
        throw new HttpError(400, "角色只能为管理员或成员");
      const [rows] = await c.execute<RowDataPacket[]>(
        "SELECT user_id FROM team_members WHERE team_id=? AND user_id=?",
        [t.id, req.params.userId],
      );
      if (!rows.length) throw new HttpError(404, "成员不存在");
      await c.execute(
        "UPDATE team_members SET role=? WHERE team_id=? AND user_id=?",
        [role, t.id, req.params.userId],
      );
    });
    res.json({ success: true });
  }),
);
teamsRouter.delete(
  "/teams/:key/members/:userId",
  asyncRoute(async (req, res) => {
    await workspaceTransaction(true, async (c) => {
      const t = await teamRow(c, req, req.params.key);
      const id =
        req.params.userId === "me" ? currentUser(req).id : req.params.userId;
      const [rows] = await c.execute<RowDataPacket[]>(
        "SELECT role FROM team_members WHERE team_id=? AND user_id=?",
        [t.id, id],
      );
      if (!rows.length) throw new HttpError(404, "成员不存在");
      if (id === t.owner_id)
        throw new HttpError(403, "所有者不能退出或被移除，请先转让普通团队");
      if (
        id !== currentUser(req).id &&
        (t.role === "member" ||
          (t.role === "admin" && rows[0].role === "admin"))
      )
        throw new HttpError(403, "没有移除此成员的权限");
      await c.execute(
        "DELETE pm FROM project_members pm JOIN projects p ON p.id=pm.project_id WHERE p.team_id=? AND pm.user_id=?",
        [t.id, id],
      );
      await c.execute(
        "DELETE FROM team_members WHERE team_id=? AND user_id=?",
        [t.id, id],
      );
    });
    res.json({ success: true });
  }),
);
teamsRouter.get(
  "/teams/:key/invites",
  asyncRoute(async (req, res) => {
    res.json(
      await workspaceTransaction(false, async (c) => {
        const t = await teamRow(c, req, req.params.key, true);
        const [rows] = await c.execute<RowDataPacket[]>(
          "SELECT id,created_at,revoked_at FROM team_invites WHERE team_id=? ORDER BY created_at DESC",
          [t.id],
        );
        return rows;
      }),
    );
  }),
);
teamsRouter.post(
  "/teams/:key/invites",
  asyncRoute(async (req, res) => {
    res.status(201).json(
      await workspaceTransaction(true, async (c) => {
        const t = await teamRow(c, req, req.params.key, true);
        const id = randomUUID(),
          token = randomBytes(32).toString("base64url");
        await c.execute(
          "INSERT INTO team_invites (id,team_id,token_hash,created_by) VALUES (?,?,?,?)",
          [id, t.id, digest(token), currentUser(req).id],
        );
        return { id, token, created_at: new Date().toISOString() };
      }),
    );
  }),
);
teamsRouter.delete(
  "/teams/:key/invites/:id",
  asyncRoute(async (req, res) => {
    await workspaceTransaction(true, async (c) => {
      const t = await teamRow(c, req, req.params.key, true);
      const [rows] = await c.execute<RowDataPacket[]>(
        "SELECT id FROM team_invites WHERE id=? AND team_id=?",
        [req.params.id, t.id],
      );
      if (!rows.length) throw new HttpError(404, "邀请不存在");
      await c.execute(
        "UPDATE team_invites SET revoked_at=COALESCE(revoked_at,CURRENT_TIMESTAMP()) WHERE id=?",
        [req.params.id],
      );
    });
    res.json({ success: true });
  }),
);
const requestColumns = `r.id,t.team_key,t.name AS team_name,r.user_id,u.name,r.message,r.status,r.created_at,r.reviewed_at`;
async function requestResult(c: any, id: string) {
  const [rows] = await c.execute(
    `SELECT ${requestColumns} FROM team_join_requests r JOIN teams t ON t.id=r.team_id JOIN users u ON u.id=r.user_id WHERE r.id=?`,
    [id],
  );
  return rows[0];
}
teamsRouter.post(
  "/team-join-requests",
  inviteLimit,
  asyncRoute(async (req, res) => {
    const b = bodyObject(req.body),
      user = currentUser(req);
    if (typeof b.message !== "string" || !b.message.trim())
      throw new HttpError(400, "请填写申请信息");
    const message = b.message.trim();
    if (Array.from(message).length > 1000)
      throw new HttpError(400, "申请信息最多1000个字符");
    res.json(
      await workspaceTransaction(true, async (c) => {
        let rows: RowDataPacket[];
        if (
          typeof b.invite_token === "string" &&
          /^[A-Za-z0-9_-]{43}$/.test(b.invite_token)
        ) {
          [rows] = await c.execute<RowDataPacket[]>(
            "SELECT t.id FROM teams t JOIN team_invites i ON i.team_id=t.id WHERE i.token_hash=? AND i.revoked_at IS NULL",
            [digest(b.invite_token)],
          );
        } else if (
          typeof b.team_code === "string" &&
          /^\d{10}$/.test(b.team_code)
        ) {
          [rows] = await c.execute<RowDataPacket[]>(
            "SELECT id FROM teams WHERE team_code=?",
            [b.team_code],
          );
        } else throw new HttpError(400, "请提供有效的团队编号或邀请链接");
        if (!rows[0]) throw new HttpError(404, "团队或邀请链接不存在");
        const teamId = rows[0].id;
        const [membership] = await c.execute<RowDataPacket[]>(
          "SELECT user_id FROM team_members WHERE team_id=? AND user_id=?",
          [teamId, user.id],
        );
        if (membership.length) throw new HttpError(409, "你已经是该团队成员");
        const [prior] = await c.execute<RowDataPacket[]>(
          "SELECT id,status FROM team_join_requests WHERE team_id=? AND user_id=?",
          [teamId, user.id],
        );
        if (prior[0]?.status === "pending")
          return requestResult(c, prior[0].id);
        const id = randomUUID();
        if (prior.length)
          await c.execute(
            "UPDATE team_join_requests SET id=?,message=?,status='pending',created_at=CURRENT_TIMESTAMP(),reviewed_at=NULL,reviewed_by=NULL WHERE id=?",
            [id, message, prior[0].id],
          );
        else
          await c.execute(
            "INSERT INTO team_join_requests (id,team_id,user_id,message) VALUES (?,?,?,?)",
            [id, teamId, user.id, message],
          );
        return requestResult(c, id);
      }),
    );
  }),
);
teamsRouter.get(
  "/team-join-requests",
  asyncRoute(async (req, res) => {
    res.json(
      await workspaceTransaction(false, async (c) => {
        const [rows] = await c.execute<RowDataPacket[]>(
          `SELECT ${requestColumns} FROM team_join_requests r JOIN teams t ON t.id=r.team_id JOIN users u ON u.id=r.user_id WHERE r.user_id=? ORDER BY r.created_at DESC`,
          [currentUser(req).id],
        );
        return rows;
      }),
    );
  }),
);
teamsRouter.get(
  "/teams/:key/join-requests",
  asyncRoute(async (req, res) => {
    res.json(
      await workspaceTransaction(false, async (c) => {
        const t = await teamRow(c, req, req.params.key, true);
        const [rows] = await c.execute<RowDataPacket[]>(
          `SELECT ${requestColumns} FROM team_join_requests r JOIN teams t ON t.id=r.team_id JOIN users u ON u.id=r.user_id WHERE r.team_id=? ORDER BY (r.status='pending') DESC,r.created_at DESC`,
          [t.id],
        );
        return rows;
      }),
    );
  }),
);
teamsRouter.patch(
  "/teams/:key/join-requests/:id",
  asyncRoute(async (req, res) => {
    const status = bodyObject(req.body).status;
    if (status !== "approved" && status !== "rejected")
      throw new HttpError(400, "审核结果不合法");
    res.json(
      await workspaceTransaction(true, async (c) => {
        const t = await teamRow(c, req, req.params.key, true);
        const [rows] = await c.execute<RowDataPacket[]>(
          "SELECT * FROM team_join_requests WHERE id=? AND team_id=?",
          [req.params.id, t.id],
        );
        const r = rows[0];
        if (!r) throw new HttpError(404, "申请不存在");
        if (r.status !== "pending") {
          if (r.status === status) return requestResult(c, r.id);
          throw new HttpError(409, "申请已经处理");
        }
        if (status === "approved")
          await c.execute(
            "INSERT IGNORE INTO team_members (team_id,user_id,role) VALUES (?,?,'member')",
            [t.id, r.user_id],
          );
        await c.execute(
          "UPDATE team_join_requests SET status=?,reviewed_by=?,reviewed_at=CURRENT_TIMESTAMP() WHERE id=?",
          [status, currentUser(req).id, r.id],
        );
        return requestResult(c, r.id);
      }),
    );
  }),
);
