import { Router, Request, Response } from "express";
import multer from "multer";
import crypto from "crypto";
import fs from "fs";
import path from "path";
import mime from "mime-types";
import { getPool } from "../db";
import { STATIC_DIR } from "../index";

export const uploadRouter = Router();

// Multer: save to temp directory, limit 200MB
const upload = multer({
  dest: "/tmp/uploads/",
  limits: { fileSize: 200 * 1024 * 1024 },
});

// In-memory upload progress tracking (for potential future chunked upload)
const uploadProgress = new Map<
  string,
  {
    uploadId: string;
    status: "UPLOADING" | "COMPLETED" | "FAILED";
    totalBytes: number;
    persistedBytes: number;
    uploadedPercent: number;
  }
>();

/** Compute SHA-256 hash of a file */
function computeFileHash(filePath: string): Promise<string> {
  return new Promise((resolve, reject) => {
    const hash = crypto.createHash("sha256");
    const stream = fs.createReadStream(filePath);
    stream.on("data", (data) => hash.update(data));
    stream.on("end", () => resolve(hash.digest("hex")));
    stream.on("error", reject);
  });
}

/**
 * POST /api/upload
 * FormData: fileKey (string), file (File), file_name? (string), mime? (string)
 * Returns: { hash: string }
 */
uploadRouter.post(
  "/upload",
  upload.single("file"),
  async (req: Request, res: Response) => {
    try {
      const fileKey = req.body.fileKey as string;
      const file = req.file;

      if (!fileKey || !file) {
        res.status(400).json({ error: "fileKey and file are required" });
        return;
      }

      const pool = getPool();

      // Verify document exists
      const [docs] = await pool.execute(
        "SELECT id FROM documents WHERE file_key = ?",
        [fileKey],
      );
      if ((docs as any[]).length === 0) {
        fs.unlinkSync(file.path);
        res.status(404).json({ error: "Document not found" });
        return;
      }

      // Compute hash
      const hash = await computeFileHash(file.path);
      const staticPath = path.join(STATIC_DIR, hash);

      // Deduplication: only save if not already on disk
      if (!fs.existsSync(staticPath)) {
        fs.renameSync(file.path, staticPath);
      } else {
        fs.unlinkSync(file.path);
      }

      const mimeType =
        (req.body.mime as string) ||
        file.mimetype ||
        mime.lookup(file.originalname) ||
        "application/octet-stream";
      const fileName = (req.body.file_name as string) || file.originalname;

      // Track progress as completed
      const progressKey = `${fileKey}:${hash}`;
      const uploadId = `upl_${Date.now()}`;
      uploadProgress.set(progressKey, {
        uploadId,
        status: "COMPLETED",
        totalBytes: file.size,
        persistedBytes: file.size,
        uploadedPercent: 100,
      });

      // Insert file record
      await pool.execute(
        "INSERT INTO files (file_key, hash, name, mime, size) VALUES (?, ?, ?, ?, ?)",
        [fileKey, hash, fileName, mimeType, file.size],
      );

      res.json({ hash });
    } catch (err) {
      console.error("[Upload Error]", err);
      res.status(500).json({ error: "Upload failed" });
    }
  },
);

/**
 * GET /api/files/:fileKey
 * Returns: { hash, name, mime }[]
 */
uploadRouter.get("/files/:fileKey", async (req: Request, res: Response) => {
  try {
    const { fileKey } = req.params;
    const pool = getPool();

    const [rows] = await pool.execute(
      "SELECT hash, name, mime FROM files WHERE file_key = ?",
      [fileKey],
    );

    res.json(rows);
  } catch (err) {
    console.error("[List Files Error]", err);
    res.status(500).json({ error: "Failed to list files" });
  }
});

/**
 * GET /api/download/:fileKey/:hash
 * Returns: blob (file stream)
 */
uploadRouter.get(
  "/download/:fileKey/:hash",
  async (req: Request, res: Response) => {
    try {
      const { fileKey, hash } = req.params;
      const pool = getPool();

      const [rows] = await pool.execute(
        "SELECT name, mime FROM files WHERE file_key = ? AND hash = ?",
        [fileKey, hash],
      );

      const fileRecord = (rows as any[])[0];
      if (!fileRecord) {
        res.status(404).json({ error: "File not found" });
        return;
      }

      const filePath = path.join(STATIC_DIR, hash);
      if (!fs.existsSync(filePath)) {
        res.status(404).json({ error: "Physical file not found" });
        return;
      }

      res.setHeader("Content-Type", fileRecord.mime);
      res.setHeader(
        "Content-Disposition",
        `attachment; filename="${encodeURIComponent(fileRecord.name)}"`,
      );

      const stream = fs.createReadStream(filePath);
      stream.pipe(res);
    } catch (err) {
      console.error("[Download Error]", err);
      res.status(500).json({ error: "Download failed" });
    }
  },
);

/**
 * GET /api/upload-progress/:fileKey/:hash
 * Returns: upload progress object
 */
uploadRouter.get(
  "/upload-progress/:fileKey/:hash",
  async (req: Request, res: Response) => {
    try {
      const { fileKey, hash } = req.params;
      const progressKey = `${fileKey}:${hash}`;

      const progress = uploadProgress.get(progressKey);
      if (!progress) {
        res.status(404).json({ error: "Upload record not found" });
        return;
      }

      res.json(progress);
    } catch (err) {
      console.error("[Progress Error]", err);
      res.status(500).json({ error: "Failed to get progress" });
    }
  },
);

/**
 * DELETE /api/attachment/:fileKey/:hash
 * Deletes an attachment record and its physical file if no other references exist
 * Returns: { success: true }
 */
uploadRouter.delete(
  "/attachment/:fileKey/:hash",
  async (req: Request, res: Response) => {
    try {
      const { fileKey, hash } = req.params;
      const pool = getPool();

      // Check if the file record exists
      const [rows] = await pool.execute(
        "SELECT id FROM files WHERE file_key = ? AND hash = ?",
        [fileKey, hash],
      );
      if ((rows as any[]).length === 0) {
        res.status(404).json({ error: "Attachment not found" });
        return;
      }

      // Delete the file record
      await pool.execute("DELETE FROM files WHERE file_key = ? AND hash = ?", [
        fileKey,
        hash,
      ]);

      // Check if this hash is still referenced by other records
      const [remaining] = await pool.execute(
        "SELECT COUNT(*) as count FROM files WHERE hash = ?",
        [hash],
      );
      const count = (remaining as any[])[0].count;

      // If no other references, delete the physical file
      if (count === 0) {
        const filePath = path.join(STATIC_DIR, hash);
        if (fs.existsSync(filePath)) {
          fs.unlinkSync(filePath);
        }
      }

      res.json({ success: true });
    } catch (err) {
      console.error("[Delete Attachment Error]", err);
      res.status(500).json({ error: "Failed to delete attachment" });
    }
  },
);
