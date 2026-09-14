import { Router } from "express";
import type { Request, Response } from "express";
import type { PoolConnection, RowDataPacket } from "mysql2/promise";
import { randomBytes, randomUUID } from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";
import { rateLimit } from "express-rate-limit";
import { asyncRoute, bodyObject, HttpError, keyValue } from "../http";
import { currentUser, digest, optionalAuth } from "../auth/session";
import { workspaceTransaction } from "../workspace/transaction";
import {
  DOCUMENT_COLUMNS,
  DOCUMENT_FROM,
  findDocument,
} from "../workspace/service";
import { resolveProjectPermissions } from "../permissions/project";
import { documentDto, type DocumentRow } from "../workspace/types";
import { STATIC_DIR, storageDirectory } from "../storage";

export const sharesRouter = Router();
export const publicSharesRouter = Router();
const shareLimit = rateLimit({
  windowMs: 60000,
  limit: 180,
  standardHeaders: "draft-8",
  legacyHeaders: false,
  message: { error: "分享访问过于频繁，请稍后重试" },
});
publicSharesRouter.use(
  "/shared-file",
  shareLimit,
  optionalAuth,
  (_req, res, next) => {
    res.setHeader("Referrer-Policy", "no-referrer");
    res.setHeader("Cache-Control", "no-store");
    next();
  },
);
sharesRouter.get(
  "/document/:key/shares",
  asyncRoute(async (req, res) => {
    res.json(
      await workspaceTransaction(false, async (c) => {
        const doc = await findDocument(
          c,
          req,
          keyValue(req.params.key, "文件标识"),
          "share",
        );
        const permissions = await resolveProjectPermissions(
          req,
          { kind: "project", project_key: doc.project_key },
          c,
        );
        const [rows] = await c.execute<RowDataPacket[]>(
          "SELECT s.id,s.permission,s.created_at,s.revoked_at,u.id AS creator_id,u.name,u.avator FROM file_shares s JOIN users u ON u.id=s.created_by WHERE s.document_id=? ORDER BY s.created_at DESC,s.id",
          [doc.id],
        );
        return rows.map((row) => ({
          id: row.id,
          permission: row.permission,
          created_at: row.created_at,
          revoked_at: row.revoked_at,
          created_by: {
            id: row.creator_id,
            name: row.name,
            avator: row.avator,
          },
          can_revoke:
            permissions.manage || row.creator_id === currentUser(req).id,
        }));
      }),
    );
  }),
);
sharesRouter.post(
  "/document/:key/shares",
  asyncRoute(async (req, res) => {
    const permission = bodyObject(req.body).permission;
    if (permission !== "read" && permission !== "edit")
      throw new HttpError(400, "分享权限只能为只读或编辑");
    res.status(201).json(
      await workspaceTransaction(true, async (c) => {
        const doc = await findDocument(
          c,
          req,
          keyValue(req.params.key, "文件标识"),
          "share",
        );
        const id = randomUUID(),
          token = randomBytes(32).toString("base64url");
        await c.execute(
          "INSERT INTO file_shares (id,document_id,token_hash,permission,created_by) VALUES (?,?,?,?,?)",
          [id, doc.id, digest(token), permission, currentUser(req).id],
        );
        return { id, token, permission, created_at: new Date().toISOString() };
      }),
    );
  }),
);
sharesRouter.delete(
  "/document/:key/shares/:id",
  asyncRoute(async (req, res) => {
    await workspaceTransaction(true, async (c) => {
      const doc = await findDocument(
        c,
        req,
        keyValue(req.params.key, "文件标识"),
        "share",
      );
      const [rows] = await c.execute<RowDataPacket[]>(
        "SELECT created_by FROM file_shares WHERE id=? AND document_id=?",
        [req.params.id, doc.id],
      );
      if (!rows[0]) throw new HttpError(404, "分享记录不存在");
      const permissions = await resolveProjectPermissions(
        req,
        { kind: "project", project_key: doc.project_key },
        c,
      );
      if (!permissions.manage && rows[0].created_by !== currentUser(req).id)
        throw new HttpError(403, "只能撤销自己创建的分享");
      await c.execute(
        "UPDATE file_shares SET revoked_at=COALESCE(revoked_at,CURRENT_TIMESTAMP()) WHERE id=?",
        [req.params.id],
      );
    });
    res.json({ success: true });
  }),
);
async function shared(c: PoolConnection, req: Request) {
  const token = req.get("x-share-token");
  if (!token || !/^[A-Za-z0-9_-]{43}$/.test(token))
    throw new HttpError(404, "分享链接无效或已撤销");
  const [rows] = await c.execute<
    (DocumentRow & { permission: "read" | "edit" })[]
  >(
    `SELECT ${DOCUMENT_COLUMNS},d.file_content,s.permission ${DOCUMENT_FROM} JOIN file_shares s ON s.document_id=d.id WHERE s.token_hash=? AND s.revoked_at IS NULL`,
    [digest(token)],
  );
  const doc = rows[0];
  if (!doc) throw new HttpError(404, "分享链接无效或已撤销");
  let write = doc.permission === "edit";
  if (req.auth) {
    const permissions = await resolveProjectPermissions(
      req,
      { kind: "project", project_key: doc.project_key },
      c,
    );
    write = write || permissions.write;
  }
  return {
    doc,
    permissions: { read: true, write, manage: false, share: false },
  };
}
publicSharesRouter.get(
  "/shared-file",
  asyncRoute(async (req, res) => {
    res.json(
      await workspaceTransaction(false, async (c) => {
        const { doc, permissions } = await shared(c, req);
        const [attachments] = await c.execute<RowDataPacket[]>(
          "SELECT f.hash,f.name,f.mime,u.id AS creator_id,u.name AS creator_name FROM files f LEFT JOIN users u ON u.id=f.creator_id WHERE f.file_key=? ORDER BY f.id",
          [doc.file_key],
        );
        // A public capability never exposes parent project/folder/team identifiers or member/contact data.
        return {
          file_key: doc.file_key,
          file_name: doc.file_name,
          file_content: doc.file_content ?? "",
          created_at: doc.created_at,
          updated_at: doc.updated_at,
          revision: Number(doc.revision),
          privileges: { mode: "inherit" },
          permissions,
          creator: {
            id: doc.creator_id,
            name: doc.creator_name,
            avator: doc.creator_avator ? "/api/shared-file/creator-avator" : "",
          },
          attachments: attachments.map((file) => ({
            hash: file.hash,
            name: file.name,
            mime: file.mime,
            creator: {
              id: file.creator_id,
              name: file.creator_name,
              avator: "",
            },
          })),
        };
      }),
    );
  }),
);
publicSharesRouter.put(
  "/shared-file",
  asyncRoute(async (req, res) => {
    const body = bodyObject(req.body);
    if (
      typeof body.file_content !== "string" ||
      !Number.isInteger(body.revision) ||
      Number(body.revision) < 1 ||
      Object.keys(body).some(
        (key) => !["file_content", "revision"].includes(key),
      )
    )
      throw new HttpError(400, "分享编辑仅允许提交正文和版本号");
    const revision = await workspaceTransaction(true, async (c) => {
      const { doc, permissions } = await shared(c, req);
      if (!permissions.write)
        throw new HttpError(403, "此分享链接只有只读权限");
      if (Number(body.revision) !== Number(doc.revision))
        throw new HttpError(
          409,
          "文件已被其他人修改，请保留本地内容后重新加载",
        );
      await c.execute(
        "UPDATE documents SET file_content=?,revision=revision+1 WHERE id=?",
        [body.file_content as string, doc.id],
      );
      return Number(doc.revision) + 1;
    });
    res.json({ success: true, revision });
  }),
);
async function sendAsset(
  res: Response,
  filePath: string,
  name: string,
  mime: string,
  inline = false,
) {
  const stat = await fs.lstat(filePath).catch(() => undefined);
  if (!stat?.isFile() || stat.isSymbolicLink())
    throw new HttpError(404, "文件不存在");
  res.setHeader("Content-Type", inline ? mime : "application/octet-stream");
  res.setHeader("X-Content-Type-Options", "nosniff");
  res.setHeader("Content-Security-Policy", "default-src 'none'; sandbox");
  res.setHeader("Cache-Control", "no-store");
  res.setHeader(
    "Content-Disposition",
    `${inline ? "inline" : "attachment"}; filename*=UTF-8''${encodeURIComponent(name).replace(/'/g, "%27")}`,
  );
  res.sendFile(filePath);
}
publicSharesRouter.get(
  "/shared-file/attachments/:hash",
  asyncRoute(async (req, res) => {
    const hash = req.params.hash;
    if (!/^[a-f0-9]{64}$/.test(hash)) throw new HttpError(404, "附件不存在");
    const file = await workspaceTransaction(false, async (c) => {
      const { doc } = await shared(c, req);
      const [rows] = await c.execute<RowDataPacket[]>(
        "SELECT name,mime FROM files WHERE file_key=? AND hash=?",
        [doc.file_key, hash],
      );
      if (!rows[0]) throw new HttpError(404, "附件不存在");
      return rows[0];
    });
    await sendAsset(res, path.join(STATIC_DIR, hash), file.name, file.mime);
  }),
);
publicSharesRouter.get(
  "/shared-file/creator-avator",
  asyncRoute(async (req, res) => {
    const asset = await workspaceTransaction(false, async (c) => {
      const { doc } = await shared(c, req);
      const match = /^\/api\/assets\/([a-f0-9-]{36})$/.exec(
        doc.creator_avator ?? "",
      );
      if (!match) throw new HttpError(404, "没有头像");
      const [rows] = await c.execute<RowDataPacket[]>(
        "SELECT * FROM assets WHERE id=? AND creator_id=? AND directory='avator' AND mime='image/webp'",
        [match[1], doc.creator_id],
      );
      if (!rows[0]) throw new HttpError(404, "没有头像");
      return rows[0];
    });
    if (!/^[a-f0-9-]{36}\.webp$/.test(asset.storage_name))
      throw new HttpError(404, "没有头像");
    await sendAsset(
      res,
      path.join(storageDirectory("avator"), asset.storage_name),
      "avator.webp",
      "image/webp",
      true,
    );
  }),
);
