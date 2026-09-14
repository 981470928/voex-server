import express from "express";
import cors from "cors";
import { initDatabase } from "./db";
import { uploadRouter } from "./routes/upload";
import { documentRouter } from "./routes/document";
import { workspaceRouter } from "./routes/workspace";
import { authRouter } from "./routes/auth";
import { teamsRouter, publicTeamsRouter } from "./routes/teams";
import { sharesRouter, publicSharesRouter } from "./routes/shares";
import { assetsRouter } from "./routes/assets";
import { protectOrigin, requireAuth, trustedOrigin } from "./auth/session";
import { initializeStorage } from "./storage";
import { errorHandler, HttpError } from "./http";

async function main() {
  initializeStorage();
  await initDatabase();
  const app = express();
  app.disable("x-powered-by");
  app.set("trust proxy", "loopback");
  app.use(
    cors((req, callback) => {
      const request = req as express.Request;
      const origin = request.get("origin");
      callback(null, {
        origin: !origin || trustedOrigin(request, origin),
        credentials: true,
      });
    }),
  );
  app.use(protectOrigin);
  app.use(express.json({ limit: "2mb" }));
  app.use("/api", (_req, res, next) => {
    res.setHeader("Cache-Control", "no-store");
    res.setHeader("X-Content-Type-Options", "nosniff");
    next();
  });
  app.get("/api/health", (_req, res) => {
    res.json({ status: "ok" });
  });
  app.use("/api", authRouter, publicTeamsRouter, publicSharesRouter);
  app.use(
    "/api",
    requireAuth,
    teamsRouter,
    sharesRouter,
    assetsRouter,
    uploadRouter,
    workspaceRouter,
    documentRouter,
  );
  app.use("/api", (_req, _res, next) => {
    next(new HttpError(404, "接口不存在"));
  });
  app.use(errorHandler);
  app.listen(8090, "127.0.0.1", () => {
    console.log("[Server] Listening on 127.0.0.1:8090");
  });
}
main().catch(() => {
  console.error("[Server] 启动失败，请检查数据库和迁移状态");
  process.exit(1);
});
