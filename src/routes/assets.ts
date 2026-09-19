import { Router } from "express";
import type { RowDataPacket } from "mysql2/promise";
import { randomUUID } from "node:crypto";
import fs from "node:fs/promises";
import path from "node:path";
import multer from "multer";
import sharp from "sharp";
import { getPool } from "../db";
import { asyncRoute, HttpError } from "../http";
import { creator, currentUser } from "../auth/session";
import {
  removeTemp,
  safeDirectory,
  storeFile,
  streamStoredFile,
  TEMP_DIR,
} from "../storage";

export const assetsRouter = Router();
const upload = multer({
  dest: TEMP_DIR,
  limits: {
    fileSize: 200 * 1024 * 1024,
    files: 1,
    fields: 1,
    parts: 3,
    fieldSize: 255,
    fieldNameSize: 30,
  },
});
assetsRouter.post(
  "/assets/upload",
  upload.single("file"),
  asyncRoute(async (req, res) => {
    const user = currentUser(req);
    const file = req.file;
    let processedPath: string | undefined;
    try {
      if (!file || !file.size) throw new HttpError(400, "请选择非空文件");
      const directory = safeDirectory(req.body?.path);
      const id = randomUUID();
      let mime = "application/octet-stream";
      let size = file.size;
      if (directory === "avator" || directory === "thumbnail") {
        const maximum = directory === "avator" ? 5 : 20;
        if (file.size > maximum * 1024 * 1024)
          throw new HttpError(413, `图片不能超过 ${maximum} MB`);
        try {
          const options = {
            limitInputPixels: 20_000_000,
            failOn: "error" as const,
          };
          const metadata = await sharp(file.path, options).metadata();
          if (
            !metadata.format ||
            !["jpeg", "png", "webp"].includes(metadata.format) ||
            (metadata.pages ?? 1) > 1
          )
            throw new Error();
          processedPath = path.join(TEMP_DIR, id + ".webp");
          await sharp(file.path, options)
            .rotate()
            .resize({
              width: directory === "avator" ? 512 : 1920,
              height: directory === "avator" ? 512 : 1920,
              fit: "inside",
              withoutEnlargement: true,
            })
            .webp({ quality: 85 })
            .toFile(processedPath);
          mime = "image/webp";
          size = (await fs.stat(processedPath)).size;
        } catch {
          throw new HttpError(
            400,
            "请选择有效的静态 PNG、JPEG 或 WebP 图片（最多 2000 万像素）",
          );
        }
      }
      const hash = await storeFile(processedPath ?? file.path);
      const name = Array.from(file.originalname).slice(0, 255).join("");
      await getPool().execute(
        "INSERT INTO assets (id, creator_id, directory, storage_name, name, mime, size) VALUES (?, ?, ?, ?, ?, ?, ?)",
        [id, user.id, directory, hash, name, mime, size],
      );
      res
        .status(201)
        .json({
          id,
          name,
          url: `/api/assets/${id}`,
          path: directory,
          size,
          mime,
          creator: creator(user),
        });
    } finally {
      await removeTemp(file?.path);
      await removeTemp(processedPath);
    }
  }),
);
assetsRouter.get(
  "/assets/:id",
  asyncRoute(async (req, res) => {
    const user = currentUser(req);
    const [rows] = await getPool().execute<RowDataPacket[]>(
      `SELECT a.* FROM assets a WHERE a.id=? AND (a.creator_id=? OR (a.directory='avator' AND EXISTS (SELECT 1 FROM users u JOIN team_members owner_member ON owner_member.user_id=u.id JOIN team_members viewer_member ON viewer_member.team_id=owner_member.team_id WHERE u.id=a.creator_id AND u.avator=CONCAT('/api/assets/',a.id) AND viewer_member.user_id=?)))`,
      [req.params.id, user.id, user.id],
    );
    if (!rows[0]) throw new HttpError(404, "文件不存在");
    const row = rows[0];
    if (!/^[a-f0-9]{64}$/.test(row.storage_name))
      throw new HttpError(404, "文件不存在");
    res.setHeader("Content-Type", row.mime);
    res.setHeader("X-Content-Type-Options", "nosniff");
    res.setHeader("Content-Security-Policy", "default-src 'none'; sandbox");
    res.setHeader("Cross-Origin-Resource-Policy", "same-origin");
    res.setHeader("Cache-Control", "private, no-store");
    res.setHeader(
      "Content-Disposition",
      `${row.mime === "image/webp" ? "inline" : "attachment"}; filename*=UTF-8''${encodeURIComponent(row.name).replace(/'/g, "%27")}`,
    );
    await streamStoredFile(row.storage_name, req, res);
  }),
);
