import { Router, Request, Response } from "express";
import { v4 as uuidv4 } from "uuid";
import fs from "fs";
import path from "path";
import { getPool } from "../db";
import { STATIC_DIR } from "../index";

export const documentRouter = Router();

/**
 * POST /api/document
 * Body: { file_name?: string }
 * Returns: { file_key: string, file_name: string }
 */
documentRouter.post("/document", async (req: Request, res: Response) => {
  try {
    const pool = getPool();
    const fileKey = uuidv4().replace(/-/g, "").slice(0, 16);
    const fileName = req.body.file_name || "untitled.md";

    await pool.execute(
      "INSERT INTO documents (file_key, file_name) VALUES (?, ?)",
      [fileKey, fileName],
    );

    res.json({ file_key: fileKey, file_name: fileName });
  } catch (err) {
    console.error("[Create Document Error]", err);
    res.status(500).json({ error: "Failed to create document" });
  }
});

/**
 * DELETE /api/document/:fileKey
 * Deletes document, associated file records, and orphaned physical files
 * Returns: { success: true }
 */
documentRouter.delete(
  "/document/:fileKey",
  async (req: Request, res: Response) => {
    try {
      const { fileKey } = req.params;
      const pool = getPool();
      const connection = await pool.getConnection();

      try {
        await connection.beginTransaction();

        // Get all file hashes for this document
        const [fileRows] = await connection.execute(
          "SELECT hash FROM files WHERE file_key = ?",
          [fileKey],
        );
        const files = fileRows as { hash: string }[];

        // Delete document
        await connection.execute("DELETE FROM documents WHERE file_key = ?", [
          fileKey,
        ]);

        // Delete file records
        await connection.execute("DELETE FROM files WHERE file_key = ?", [
          fileKey,
        ]);

        await connection.commit();

        // Check each hash: delete physical file only if no other references exist
        for (const file of files) {
          const [remaining] = await pool.execute(
            "SELECT COUNT(*) as count FROM files WHERE hash = ?",
            [file.hash],
          );
          const count = (remaining as any[])[0].count;
          if (count === 0) {
            const filePath = path.join(STATIC_DIR, file.hash);
            if (fs.existsSync(filePath)) {
              fs.unlinkSync(filePath);
            }
          }
        }

        res.json({ success: true });
      } catch (err) {
        await connection.rollback();
        throw err;
      } finally {
        connection.release();
      }
    } catch (err) {
      console.error("[Delete Document Error]", err);
      res.status(500).json({ error: "Failed to delete document" });
    }
  },
);

/**
 * PUT /api/document/:fileKey
 * Body: { file_content?: string, file_name?: string }
 * At least one of file_content or file_name is required
 * Returns: { success: true }
 */
documentRouter.put(
  "/document/:fileKey",
  async (req: Request, res: Response) => {
    try {
      const { fileKey } = req.params;
      const { file_content, file_name } = req.body;

      if (file_content === undefined && file_name === undefined) {
        res.status(400).json({
          error: "At least one of file_content or file_name is required",
        });
        return;
      }

      const pool = getPool();

      // Build dynamic SET clause
      const sets: string[] = [];
      const values: any[] = [];

      if (file_content !== undefined) {
        sets.push("file_content = ?");
        values.push(file_content);
      }
      if (file_name !== undefined) {
        sets.push("file_name = ?");
        values.push(file_name);
      }

      values.push(fileKey);

      const [result] = await pool.execute(
        `UPDATE documents SET ${sets.join(", ")} WHERE file_key = ?`,
        values,
      );

      if ((result as any).affectedRows === 0) {
        res.status(404).json({ error: "Document not found" });
        return;
      }

      res.json({ success: true });
    } catch (err) {
      console.error("[Update Document Error]", err);
      res.status(500).json({ error: "Failed to update document" });
    }
  },
);

/**
 * GET /api/documents?code=keyword
 * Without code: returns all documents
 * With code: returns documents matching file_name (LIKE search)
 * Returns: { id, file_key, file_name, file_content, created_at, updated_at }[]
 */
documentRouter.get("/documents", async (req: Request, res: Response) => {
  try {
    const pool = getPool();
    const code = req.query.code as string | undefined;

    let rows;
    if (code) {
      [rows] = await pool.execute(
        "SELECT id, file_key, file_name, file_content, created_at, updated_at FROM documents WHERE file_name LIKE ?",
        [`%${code}%`],
      );
    } else {
      [rows] = await pool.execute(
        "SELECT id, file_key, file_name, file_content, created_at, updated_at FROM documents",
      );
    }

    res.json(rows);
  } catch (err) {
    console.error("[Query Documents Error]", err);
    res.status(500).json({ error: "Failed to query documents" });
  }
});
