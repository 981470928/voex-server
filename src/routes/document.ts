import { Router } from "express";
import { creator, currentUser } from "../auth/session";
import {
  HttpError,
  asyncRoute,
  bodyObject,
  keyValue,
  nameValue,
} from "../http";
import { resolveProjectPermissions } from "../permissions/project";
import {
  DOCUMENT_COLUMNS,
  DOCUMENT_FROM,
  documentLocation,
  findDocument,
} from "../workspace/service";
import { publicKey } from "../workspace/store";
import { workspaceTransaction } from "../workspace/transaction";
import {
  documentDto,
  type DocumentInfo,
  type DocumentRow,
} from "../workspace/types";

export const documentRouter: Router = Router();

documentRouter.post(
  "/document",
  asyncRoute(async (req, res) => {
    const body = bodyObject(req.body);
    const fileName =
      body.file_name === undefined
        ? "untitled.md"
        : nameValue(body.file_name, "文档名称");
    const projectKey =
      body.project_key === undefined
        ? undefined
        : keyValue(body.project_key, "项目标识");
    const folderKey =
      body.folder_key === undefined
        ? undefined
        : keyValue(body.folder_key, "文件夹标识");
    const result = await workspaceTransaction(true, async (connection) => {
      const location = await documentLocation(
        connection,
        req,
        projectKey,
        folderKey,
        body.team_key === undefined
          ? undefined
          : keyValue(body.team_key, "团队标识"),
      );
      const fileKey = publicKey();
      await connection.execute(
        "INSERT INTO documents (file_key, file_name, folder_id, creator_id, privileges) VALUES (?, ?, ?, ?, JSON_OBJECT('mode','inherit'))",
        [fileKey, fileName, location.folder.id, currentUser(req).id],
      );
      return {
        revision: 1,
        privileges: { mode: "inherit" },
        creator: creator(currentUser(req)),
        file_key: fileKey,
        file_name: fileName,
        project_key: location.project.project_key,
        folder_key: location.folder.folder_key,
        project_name: location.project.name,
        folder_path: location.folder_path,
      };
    });
    res.json(result);
  }),
);

documentRouter.get(
  "/document/:fileKey",
  asyncRoute(async (req, res) => {
    const key = keyValue(req.params.fileKey, "文档标识");
    const info = await workspaceTransaction(
      false,
      async (connection): Promise<DocumentInfo & { permissions: unknown }> => {
        const row = await findDocument(connection, req, key, "read", true);
        return {
          ...documentDto(row),
          file_content: row.file_content ?? null,
          permissions: await resolveProjectPermissions(
            req,
            { kind: "project", project_key: row.project_key },
            connection,
          ),
        };
      },
    );
    res.json(info);
  }),
);

documentRouter.put(
  "/document/:fileKey",
  asyncRoute(async (req, res) => {
    const key = keyValue(req.params.fileKey, "文档标识");
    const body = bodyObject(req.body);
    const sets: string[] = [];
    const values: (string | null)[] = [];
    if (body.file_name !== undefined) {
      sets.push("file_name = ?");
      values.push(nameValue(body.file_name, "文档名称"));
    }
    if (body.file_content !== undefined) {
      if (body.file_content !== null && typeof body.file_content !== "string") {
        throw new HttpError(400, "文档内容必须是字符串或 null");
      }
      sets.push("file_content = ?");
      values.push(body.file_content);
    }
    if (sets.length === 0) throw new HttpError(400, "请提供文档名称或文档内容");
    const revision = await workspaceTransaction(true, async (connection) => {
      const document = await findDocument(connection, req, key, "write");
      if (body.file_content !== undefined) {
        if (!Number.isInteger(body.revision))
          throw new HttpError(400, "保存正文需要版本号");
        if (body.revision !== document.revision)
          throw new HttpError(
            409,
            "文件已被其他人修改，请保留本地内容后重新加载",
          );
        sets.push("revision = revision + 1");
      }
      await connection.execute(
        `UPDATE documents SET ${sets.join(", ")} WHERE file_key = ?`,
        [...values, document.file_key],
      );
      return (
        Number(document.revision) + (body.file_content !== undefined ? 1 : 0)
      );
    });
    res.json({ success: true, revision });
  }),
);

documentRouter.delete(
  "/document/:fileKey",
  asyncRoute(async (req, res) => {
    const key = keyValue(req.params.fileKey, "文档标识");
    await workspaceTransaction(true, async (connection) => {
      const document = await findDocument(connection, req, key, "write");
      await connection.execute("DELETE FROM files WHERE file_key = ?", [
        document.file_key,
      ]);
      await connection.execute("DELETE FROM documents WHERE id = ?", [
        document.id,
      ]);
    });

    // Shared content hashes remain on disk until a separate orphan sweep.
    res.json({ success: true });
  }),
);

/** 当前用户的文档元数据列表，不包含正文。 */
documentRouter.get(
  "/documents",
  asyncRoute(async (req, res) => {
    const code = req.query.code;
    if (code !== undefined && typeof code !== "string") {
      throw new HttpError(400, "搜索关键字必须是字符串");
    }
    const result = await workspaceTransaction(false, async (connection) => {
      const [documents] = await connection.execute<DocumentRow[]>(
        `SELECT ${DOCUMENT_COLUMNS} ${DOCUMENT_FROM}
       WHERE EXISTS (SELECT 1 FROM team_members tm WHERE tm.team_id=p.team_id AND tm.user_id=?) ${code ? "AND d.file_name LIKE ?" : ""}
       ORDER BY d.created_at ASC, d.id ASC`,
        code ? [currentUser(req).id, `%${code}%`] : [currentUser(req).id],
      );
      const readable = new Map<string, boolean>();
      const visible = [];
      for (const document of documents) {
        if (!readable.has(document.project_key)) {
          const permissions = await resolveProjectPermissions(
            req,
            {
              kind: "project",
              project_key: document.project_key,
            },
            connection,
          );
          readable.set(document.project_key, permissions.read);
        }
        if (readable.get(document.project_key))
          visible.push(documentDto(document));
      }
      return visible;
    });
    res.json(result);
  }),
);
