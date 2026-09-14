import { Router } from "express";
import type { RowDataPacket } from "mysql2/promise";
import { randomUUID, randomBytes } from "node:crypto";
import argon2 from "argon2";
import { rateLimit, ipKeyGenerator } from "express-rate-limit";
import { getPool } from "../db";
import { ensurePersonalTeam } from "../teams/store";
import { asyncRoute, bodyObject, HttpError } from "../http";
import { accountValue, passwordValue, profileValues } from "../auth/validation";
import {
  clearSessionCookies,
  revokePresentedAccess,
  cookie,
  createSession,
  currentUser,
  digest,
  issueAccess,
  requireAuth,
  userDto,
  type UserRow,
} from "../auth/session";

export const authRouter = Router();
const hashOptions = {
  type: argon2.argon2id,
  memoryCost: 19456,
  timeCost: 2,
  parallelism: 1,
} as const;
const dummyHash = argon2.hash(randomBytes(32), hashOptions);
const attempts = rateLimit({
  windowMs: 15 * 60 * 1000,
  limit: 30,
  standardHeaders: "draft-8",
  legacyHeaders: false,
  skipSuccessfulRequests: true,
  message: { error: "尝试次数过多，请 15 分钟后重试" },
});
const accountAttempts = rateLimit({
  windowMs: 15 * 60 * 1000,
  limit: 10,
  standardHeaders: "draft-8",
  legacyHeaders: false,
  skipSuccessfulRequests: true,
  keyGenerator: (req) =>
    typeof req.body?.account === "string"
      ? digest(req.body.account)
      : ipKeyGenerator(req.ip || "127.0.0.1"),
  message: { error: "尝试次数过多，请 15 分钟后重试" },
});
const registrations = rateLimit({
  windowMs: 60 * 60 * 1000,
  limit: 15,
  standardHeaders: "draft-8",
  legacyHeaders: false,
  message: { error: "注册次数过多，请 1 小时后重试" },
});
const availability = rateLimit({
  windowMs: 60 * 1000,
  limit: 60,
  standardHeaders: "draft-8",
  legacyHeaders: false,
  message: { error: "检测过于频繁，请稍后重试" },
});
let hashing = 0;
async function withHashSlot<T>(operation: () => Promise<T>): Promise<T> {
  if (hashing >= 4) throw new HttpError(429, "登录服务繁忙，请稍后重试");
  hashing++;
  try {
    return await operation();
  } finally {
    hashing--;
  }
}

authRouter.post(
  "/auth/account-availability",
  availability,
  asyncRoute(async (req, res) => {
    const account = accountValue(bodyObject(req.body).account);
    const [rows] = await getPool().execute<RowDataPacket[]>(
      "SELECT id FROM users WHERE account = ?",
      [Buffer.from(account)],
    );
    res.json({ available: rows.length === 0 });
  }),
);
authRouter.post(
  "/auth/register",
  registrations,
  asyncRoute(async (req, res) => {
    const body = bodyObject(req.body);
    const account = accountValue(body.account);
    const password = passwordValue(body.password);
    const profile = profileValues(body);
    const id = randomUUID();
    const passwordHash = await withHashSlot(() =>
      argon2.hash(password, hashOptions),
    );
    const connection = await getPool().getConnection();
    try {
      await connection.beginTransaction();
      await connection.query(
        "SELECT id FROM workspace_state WHERE id=1 FOR UPDATE",
      );
      await connection.execute(
        "INSERT INTO users (id, account, password_hash, name, email, phone) VALUES (?, ?, ?, ?, ?, ?)",
        [
          id,
          Buffer.from(account),
          passwordHash,
          profile.name,
          profile.email,
          profile.phone,
        ],
      );
      await ensurePersonalTeam(connection, id, profile.name);
      const session = await createSession(
        req,
        res,
        { id, account, ...profile, avator: "" },
        connection,
      );
      await connection.commit();
      res.status(201).json(session);
    } catch (error) {
      await connection.rollback();
      clearSessionCookies(req, res);
      if ((error as { code?: string }).code === "ER_DUP_ENTRY")
        throw new HttpError(409, "账号已被使用，请更换账号");
      throw error;
    } finally {
      connection.release();
    }
  }),
);
authRouter.post(
  "/auth/login",
  attempts,
  accountAttempts,
  asyncRoute(async (req, res) => {
    const body = bodyObject(req.body);
    const account = accountValue(body.account);
    const password = passwordValue(body.password);
    const [rows] = await getPool().execute<UserRow[]>(
      "SELECT * FROM users WHERE account = ?",
      [Buffer.from(account)],
    );
    const row = rows[0];
    const valid = await withHashSlot(async () =>
      argon2.verify(row?.password_hash ?? (await dummyHash), password),
    );
    if (!row || !valid) throw new HttpError(401, "账号或密码不正确");
    // Reauthentication replaces the browser session instead of leaving the previous token active.
    const previous = cookie(req, "voex_refresh");
    if (previous)
      await getPool().execute(
        "DELETE FROM auth_sessions WHERE refresh_hash = ?",
        [digest(previous)],
      );
    res.json(await createSession(req, res, userDto(row)));
  }),
);
authRouter.post(
  "/auth/refresh",
  availability,
  asyncRoute(async (req, res) => {
    const token = cookie(req, "voex_refresh");
    if (!token || !/^[A-Za-z0-9_-]{43}$/.test(token))
      throw new HttpError(401, "请重新登录");
    const [rows] = await getPool().execute<
      (UserRow & { session_id: string })[]
    >(
      "SELECT u.*, s.id AS session_id FROM users u JOIN auth_sessions s ON s.user_id = u.id WHERE s.refresh_hash = ? AND s.expires_at > UTC_TIMESTAMP()",
      [digest(token)],
    );
    if (!rows[0]) {
      clearSessionCookies(req, res);
      throw new HttpError(401, "登录已过期，请重新登录");
    }
    res.json(issueAccess(req, res, userDto(rows[0]), rows[0].session_id));
  }),
);
authRouter.post(
  "/auth/logout",
  asyncRoute(async (req, res) => {
    await revokePresentedAccess(req);
    const token = cookie(req, "voex_refresh");
    if (token)
      await getPool().execute(
        "DELETE FROM auth_sessions WHERE refresh_hash = ?",
        [digest(token)],
      );
    clearSessionCookies(req, res);
    res.json({ success: true });
  }),
);
authRouter.get(
  "/auth/me",
  requireAuth,
  asyncRoute(async (req, res) => {
    res.json(currentUser(req));
  }),
);
authRouter.patch(
  "/auth/me",
  requireAuth,
  asyncRoute(async (req, res) => {
    const user = currentUser(req);
    const body = bodyObject(req.body);
    const profile = profileValues({
      name: user.name,
      email: user.email,
      phone: user.phone,
      ...body,
    });
    let avator = user.avator;
    if (body.avator !== undefined) {
      if (typeof body.avator !== "string")
        throw new HttpError(400, "头像格式错误");
      avator = body.avator;
      if (avator) {
        const match = /^\/api\/assets\/([a-f0-9-]{36})$/.exec(avator);
        if (!match) throw new HttpError(400, "请先上传头像");
        const [assets] = await getPool().execute<RowDataPacket[]>(
          "SELECT id FROM assets WHERE id = ? AND creator_id = ? AND directory = 'avator' AND mime IN ('image/png', 'image/jpeg', 'image/webp')",
          [match[1], user.id],
        );
        if (!assets.length)
          throw new HttpError(400, "头像文件不存在或不属于当前账号");
      }
    }
    await getPool().execute(
      "UPDATE users SET name = ?, email = ?, phone = ?, avator = ? WHERE id = ?",
      [profile.name, profile.email, profile.phone, avator, user.id],
    );
    res.json({ ...user, ...profile, avator });
  }),
);
