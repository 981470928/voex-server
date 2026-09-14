import { Router } from "express";
import { asyncRoute, bodyObject, keyValue, nameValue } from "../http";
import {
  projectPrivileges,
  setProjectPrivileges,
  createFolder,
  createProject,
  deleteFolder,
  deleteProject,
  initializeWorkspace,
  listProjects,
  projectTree,
  renameFolder,
  renameProject,
} from "../workspace/service";
import { workspaceTransaction } from "../workspace/transaction";

export const workspaceRouter: Router = Router();

workspaceRouter.get(
  "/projects",
  asyncRoute(async (req, res) => {
    res.json(
      await workspaceTransaction(false, (connection) =>
        listProjects(
          connection,
          req,
          req.query.team_key === undefined
            ? undefined
            : keyValue(req.query.team_key, "团队标识"),
        ),
      ),
    );
  }),
);

workspaceRouter.post(
  "/workspace/initialize",
  asyncRoute(async (req, res) => {
    res.json(
      await workspaceTransaction(true, (connection) =>
        initializeWorkspace(
          connection,
          req,
          req.body?.team_key === undefined
            ? undefined
            : keyValue(req.body.team_key, "团队标识"),
        ),
      ),
    );
  }),
);

workspaceRouter.post(
  "/project",
  asyncRoute(async (req, res) => {
    const name = nameValue(bodyObject(req.body).name, "项目名称");
    res.json(
      await workspaceTransaction(true, (connection) =>
        createProject(
          connection,
          req,
          name,
          req.body.team_key === undefined
            ? undefined
            : keyValue(req.body.team_key, "团队标识"),
        ),
      ),
    );
  }),
);

workspaceRouter.put(
  "/project/:projectKey",
  asyncRoute(async (req, res) => {
    const key = keyValue(req.params.projectKey, "项目标识");
    const name = nameValue(bodyObject(req.body).name, "项目名称");
    await workspaceTransaction(true, (connection) =>
      renameProject(connection, req, key, name),
    );
    res.json({ success: true });
  }),
);

workspaceRouter.delete(
  "/project/:projectKey",
  asyncRoute(async (req, res) => {
    const key = keyValue(req.params.projectKey, "项目标识");
    await workspaceTransaction(true, (connection) =>
      deleteProject(connection, req, key),
    );
    res.json({ success: true });
  }),
);

workspaceRouter.get(
  "/project/:projectKey/tree",
  asyncRoute(async (req, res) => {
    const key = keyValue(req.params.projectKey, "项目标识");
    res.json(
      await workspaceTransaction(false, (connection) =>
        projectTree(connection, req, key),
      ),
    );
  }),
);

workspaceRouter.post(
  "/folder",
  asyncRoute(async (req, res) => {
    const body = bodyObject(req.body);
    const projectKey = keyValue(body.project_key, "项目标识");
    const parentKey =
      body.parent_key === undefined || body.parent_key === null
        ? null
        : keyValue(body.parent_key, "父文件夹标识");
    const name = nameValue(body.name, "文件夹名称");
    res.json(
      await workspaceTransaction(true, (connection) =>
        createFolder(connection, req, projectKey, parentKey, name),
      ),
    );
  }),
);

workspaceRouter.put(
  "/folder/:folderKey",
  asyncRoute(async (req, res) => {
    const key = keyValue(req.params.folderKey, "文件夹标识");
    const name = nameValue(bodyObject(req.body).name, "文件夹名称");
    await workspaceTransaction(true, (connection) =>
      renameFolder(connection, req, key, name),
    );
    res.json({ success: true });
  }),
);

workspaceRouter.delete(
  "/folder/:folderKey",
  asyncRoute(async (req, res) => {
    const key = keyValue(req.params.folderKey, "文件夹标识");
    await workspaceTransaction(true, (connection) =>
      deleteFolder(connection, req, key),
    );
    res.json({ success: true });
  }),
);

workspaceRouter.get(
  "/project/:projectKey/privileges",
  asyncRoute(async (req, res) => {
    res.json(
      await workspaceTransaction(false, (c) =>
        projectPrivileges(c, req, keyValue(req.params.projectKey, "项目标识")),
      ),
    );
  }),
);
workspaceRouter.put(
  "/project/:projectKey/privileges",
  asyncRoute(async (req, res) => {
    const b = bodyObject(req.body);
    res.json(
      await workspaceTransaction(true, (c) =>
        setProjectPrivileges(
          c,
          req,
          keyValue(req.params.projectKey, "项目标识"),
          b.mode,
          b.user_ids,
        ),
      ),
    );
  }),
);
