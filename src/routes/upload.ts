import { Router } from "express";
import type { Request } from "express";
import type { PoolConnection, RowDataPacket } from "mysql2/promise";
import multer from "multer";
import { asyncRoute, HttpError, keyValue } from "../http";
import { creator, currentUser } from "../auth/session";
import { findDocument } from "../workspace/service";
import { workspaceTransaction } from "../workspace/transaction";
import { removeTemp, storeFile, streamStoredFile, TEMP_DIR } from "../storage";

export const uploadRouter = Router();
const upload = multer({
  dest: TEMP_DIR,
  limits: {
    fileSize: 200 * 1024 * 1024,
    files: 1,
    fields: 3,
    parts: 5,
    fieldSize: 4096,
  },
});
function hashValue(value: unknown): string {
  if (typeof value !== "string" || !/^[a-f0-9]{64}$/.test(value))
    throw new HttpError(400, "附件标识不合法");
  return value;
}
async function attachment(connection: PoolConnection, req: Request) {
  const fileKey = keyValue(req.params.fileKey, "文档标识");
  const hash = hashValue(req.params.hash);
  await findDocument(
    connection,
    req,
    fileKey,
    req.method === "GET" ? "read" : "write",
  );
  const [rows] = await connection.execute<RowDataPacket[]>(
    "SELECT id, hash, name, mime, size FROM files WHERE file_key = ? AND hash = ?",
    [fileKey, hash],
  );
  if (!rows[0]) throw new HttpError(404, "附件不存在");
  return { fileKey, hash, file: rows[0] };
}
uploadRouter.post(
  "/upload",
  upload.single("file"),
  asyncRoute(async (req, res) => {
    const user = currentUser(req);
    const file = req.file;
    try {
      if (!file) throw new HttpError(400, "请选择文件");
      const fileKey = keyValue(req.body.fileKey, "文档标识");
      // Verify ownership before storing the uploaded body.
      await workspaceTransaction(false, (connection) =>
        findDocument(connection, req, fileKey, "write"),
      );
      const rawName = req.body.file_name ?? file.originalname;
      const rawMime =
        req.body.mime ?? file.mimetype ?? "application/octet-stream";
      if (
        typeof rawName !== "string" ||
        !rawName ||
        Array.from(rawName).length > 255 ||
        typeof rawMime !== "string" ||
        !/^[a-zA-Z0-9.+-]+\/[a-zA-Z0-9.+-]+$/.test(rawMime) ||
        rawMime.length > 128
      )
        throw new HttpError(400, "文件名或文件类型不合法");
      const hash = await storeFile(file.path);
      await workspaceTransaction(true, async (connection) => {
        await findDocument(connection, req, fileKey, "write");
        await connection.execute(
          "INSERT INTO files (file_key, hash, name, mime, size, creator_id, privileges) VALUES (?, ?, ?, ?, ?, ?, JSON_OBJECT('mode','inherit'))",
          [fileKey, hash, rawName, rawMime, file.size, user.id],
        );
      });
      res.json({ hash, creator: creator(user) });
    } finally {
      await removeTemp(file?.path);
    }
  }),
);
uploadRouter.get(
  "/files/:fileKey",
  asyncRoute(async (req, res) => {
    const user = currentUser(req);
    const rows = await workspaceTransaction(false, async (connection) => {
      const fileKey = keyValue(req.params.fileKey, "文档标识");
      await findDocument(connection, req, fileKey, "read");
      const [files] = await connection.execute<RowDataPacket[]>(
        "SELECT f.hash,f.name,f.mime,f.creator_id,u.name AS creator_name,u.avator AS creator_avator FROM files f LEFT JOIN users u ON u.id=f.creator_id WHERE f.file_key = ?",
        [fileKey],
      );
      return files.map((file) => ({
        hash: file.hash,
        name: file.name,
        mime: file.mime,
        privileges: { mode: "inherit" },
        creator: {
          id: file.creator_id,
          name: file.creator_name,
          avator: file.creator_avator,
        },
      }));
    });
    res.json(rows);
  }),
);
uploadRouter.get(
  "/download/:fileKey/:hash",
  asyncRoute(async (req, res) => {
    const { hash, file } = await workspaceTransaction(false, (connection) =>
      attachment(connection, req),
    );
    res.attachment(file.name);
    res.setHeader("Content-Type", "application/octet-stream");
    res.setHeader("X-Content-Type-Options", "nosniff");
    res.setHeader("Content-Security-Policy", "default-src 'none'; sandbox");
    res.setHeader("Cache-Control", "private, no-store");
    await streamStoredFile(hash, req, res);
  }),
);
uploadRouter.get(
  "/upload-progress/:fileKey/:hash",
  asyncRoute(async (req, res) => {
    const { hash, file } = await workspaceTransaction(false, (connection) =>
      attachment(connection, req),
    );
    res.json({
      uploadId: hash,
      status: "COMPLETED",
      totalBytes: Number(file.size),
      persistedBytes: Number(file.size),
      uploadedPercent: 100,
    });
  }),
);
uploadRouter.delete(
  "/attachment/:fileKey/:hash",
  asyncRoute(async (req, res) => {
    await workspaceTransaction(true, async (connection) => {
      const { fileKey, hash } = await attachment(connection, req);
      await connection.execute(
        "DELETE FROM files WHERE file_key = ? AND hash = ?",
        [fileKey, hash],
      );
      // A concurrent upload can reuse a content hash; physical cleanup is left to a separate orphan sweep.
    });
    res.json({ success: true });
  }),
);
