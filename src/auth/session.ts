import type { Request, RequestHandler, Response } from "express";
import type { Pool, PoolConnection, RowDataPacket } from "mysql2/promise";
import { createHash, randomBytes, randomUUID } from "node:crypto";
import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import jwt from "jsonwebtoken";
import { getPool } from "../db";
import { HttpError } from "../http";

export interface UserRow extends RowDataPacket {
  id: string;
  account: Buffer;
  password_hash: string;
  name: string;
  avator: string;
  email: string;
  phone: string;
}
export type User = {
  id: string;
  account: string;
  name: string;
  avator: string;
  email: string;
  phone: string;
};
export type Creator = Pick<User, "id" | "name" | "avator">;
declare global {
  namespace Express {
    interface Request {
      auth?: { user: User; sessionId: string };
    }
  }
}
export const ACCESS_SECONDS = 900;
const REFRESH_SECONDS = 14 * 24 * 60 * 60;
const issuer = "voex";
const audience = "voex-web";
mkdirSync("/home/server/.secrets", { recursive: true, mode: 0o700 });
const secretPath = "/home/server/.secrets/jwt-key";
try {
  writeFileSync(secretPath, randomBytes(64), { flag: "wx", mode: 0o600 });
} catch (error) {
  if ((error as NodeJS.ErrnoException).code !== "EEXIST") throw error;
}
const secret = readFileSync(secretPath);
if (secret.length < 64) throw new Error("JWT signing key is too short");

export function userDto(row: UserRow): User {
  return {
    id: row.id,
    account: row.account.toString("utf8"),
    name: row.name,
    avator: row.avator,
    email: row.email,
    phone: row.phone,
  };
}
export function currentUser(req: Request): User {
  if (!req.auth) throw new HttpError(401, "请先登录");
  return req.auth.user;
}
export function creator(user: User): Creator {
  return { id: user.id, name: user.name, avator: user.avator };
}
export function cookie(req: Request, name: string): string | undefined {
  const raw = req.headers.cookie
    ?.split(";")
    .map((part) => part.trim())
    .find((part) => part.startsWith(name + "="));
  if (!raw) return;
  try {
    return decodeURIComponent(raw.slice(name.length + 1));
  } catch {
    return;
  }
}
export function digest(value: string): string {
  return createHash("sha256").update(value).digest("hex");
}
function cookieOptions(req: Request, path: string) {
  return {
    httpOnly: true,
    secure: req.secure,
    sameSite: "strict" as const,
    path,
  };
}
export function clearSessionCookies(req: Request, res: Response): void {
  res.clearCookie("voex_access", cookieOptions(req, "/api"));
  res.clearCookie("voex_refresh", cookieOptions(req, "/api/auth"));
}
export function issueAccess(
  req: Request,
  res: Response,
  user: User,
  sessionId: string,
) {
  const accessToken = jwt.sign({ sid: sessionId }, secret, {
    algorithm: "HS256",
    subject: user.id,
    issuer,
    audience,
    expiresIn: ACCESS_SECONDS,
  });
  res.cookie("voex_access", accessToken, {
    ...cookieOptions(req, "/api"),
    maxAge: ACCESS_SECONDS * 1000,
  });
  res.setHeader("Cache-Control", "no-store");
  return { accessToken, expiresIn: ACCESS_SECONDS, user };
}
export async function createSession(
  req: Request,
  res: Response,
  user: User,
  database: Pool | PoolConnection = getPool(),
) {
  const sessionId = randomUUID();
  const refreshToken = randomBytes(32).toString("base64url");
  await database.execute(
    "DELETE FROM auth_sessions WHERE expires_at <= UTC_TIMESTAMP()",
  );
  await database.execute(
    "INSERT INTO auth_sessions (id, user_id, refresh_hash, expires_at) VALUES (?, ?, ?, DATE_ADD(UTC_TIMESTAMP(), INTERVAL 14 DAY))",
    [sessionId, user.id, digest(refreshToken)],
  );
  res.cookie("voex_refresh", refreshToken, {
    ...cookieOptions(req, "/api/auth"),
    maxAge: REFRESH_SECONDS * 1000,
  });
  return issueAccess(req, res, user, sessionId);
}
export const requireAuth: RequestHandler = (req, res, next) => {
  void (async () => {
    const header = req.get("authorization");
    const token = header
      ? /^Bearer ([^\s]+)$/i.exec(header)?.[1]
      : cookie(req, "voex_access");
    if (!token) throw new HttpError(401, "请先登录");
    let payload: jwt.JwtPayload;
    try {
      const decoded = jwt.verify(token, secret, {
        algorithms: ["HS256"],
        issuer,
        audience,
        maxAge: ACCESS_SECONDS,
      });
      if (
        typeof decoded === "string" ||
        typeof decoded.sub !== "string" ||
        typeof decoded.sid !== "string" ||
        !decoded.exp
      )
        throw new Error();
      payload = decoded;
    } catch {
      throw new HttpError(401, "登录已过期，请重新登录");
    }
    const [rows] = await getPool().execute<UserRow[]>(
      `SELECT u.* FROM users u JOIN auth_sessions s ON s.user_id = u.id
       WHERE u.id = ? AND s.id = ? AND s.expires_at > UTC_TIMESTAMP()`,
      [payload.sub, payload.sid],
    );
    if (!rows[0]) throw new HttpError(401, "登录已失效，请重新登录");
    req.auth = { user: userDto(rows[0]), sessionId: payload.sid };
    res.setHeader("Cache-Control", "no-store");
    next();
  })().catch(next);
};

const allowedOrigins = new Set(
  (
    process.env.AUTH_ALLOWED_ORIGINS ||
    "https://voex.jmin.site,https://jmin.site,http://218.76.62.176,http://localhost:3000,http://127.0.0.1:3000,http://localhost:3001,http://127.0.0.1:3001"
  ).split(","),
);
export function trustedOrigin(req: Request, origin: string): boolean {
  return (
    origin === `${req.protocol}://${req.get("host")}` ||
    allowedOrigins.has(origin)
  );
}
export const protectOrigin: RequestHandler = (req, _res, next) => {
  if (!["GET", "HEAD", "OPTIONS"].includes(req.method)) {
    const origin = req.get("origin");
    if (origin && !trustedOrigin(req, origin)) {
      next(new HttpError(403, "请求来源不受信任"));
      return;
    }
    if (!origin && req.get("sec-fetch-site") === "cross-site") {
      next(new HttpError(403, "请求来源不受信任"));
      return;
    }
  }
  next();
};

export async function revokePresentedAccess(req: Request): Promise<void> {
  const header = req.get("authorization");
  const token = header
    ? /^Bearer ([^\s]+)$/i.exec(header)?.[1]
    : cookie(req, "voex_access");
  if (!token) return;
  let payload: jwt.JwtPayload;
  try {
    const decoded = jwt.verify(token, secret, {
      algorithms: ["HS256"],
      issuer,
      audience,
      ignoreExpiration: true,
    });
    if (
      typeof decoded === "string" ||
      typeof decoded.sub !== "string" ||
      typeof decoded.sid !== "string"
    )
      return;
    payload = decoded;
  } catch {
    return;
  }
  await getPool().execute(
    "DELETE FROM auth_sessions WHERE id = ? AND user_id = ?",
    [payload.sid, payload.sub],
  );
}

export const optionalAuth: RequestHandler = (req, res, next) => {
  if (!req.get("authorization") && !cookie(req, "voex_access")) {
    next();
    return;
  }
  requireAuth(req, res, (error?: unknown) => {
    if (error instanceof HttpError && error.status === 401) {
      next();
      return;
    }
    next(error);
  });
};
