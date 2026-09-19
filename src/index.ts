import { Console } from "node:console";
import express from "express";
import cors from "cors";
import morgan from "morgan";
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

// Runtime diagnostics stay separate from the HTTP access log.
globalThis.console = new Console({ stdout: process.stderr, stderr: process.stderr });

async function main() {
  initializeStorage();
  const pool = await initDatabase();
  const app = express();
  app.disable("x-powered-by");
  app.set("trust proxy", "loopback");
  morgan.token("path", (req) => req.url?.split("?", 1)[0] ?? "-");
  app.use(
    morgan(
      "[:date[iso]] :remote-addr :method :path :status :response-time[3] ms",
      { stream: process.stdout },
    ),
  );
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
  const server = app.listen(8090, "127.0.0.1", () => {
    console.log("[Server] HTTP listening on 127.0.0.1:8090");
  });
  server.on("error", (error) => {
    console.error("[Server] HTTP listener failed:", error);
    void pool.end().finally(() => process.exit(1));
  });

  let stopping = false;
  const stop = (signal: string) => {
    if (stopping) return;
    stopping = true;
    console.log("[Server] Shutdown requested:", signal);
    const deadline = setTimeout(() => {
      console.error("[Server] Shutdown exceeded 25 seconds");
      server.closeAllConnections();
      process.exit(1);
    }, 25_000);
    deadline.unref();
    server.close((error) => {
      if (error) console.error("[Server] HTTP shutdown failed:", error);
      void pool.end().then(
        () => {
          clearTimeout(deadline);
          console.log("[Server] HTTP listener and database pool closed");
          process.exitCode = error ? 1 : 0;
        },
        (poolError) => {
          console.error("[Server] Database shutdown failed:", poolError);
          process.exit(1);
        },
      );
    });
  };
  process.on("SIGINT", () => stop("SIGINT"));
  process.on("SIGTERM", () => stop("SIGTERM"));
  process.on("SIGBREAK", () => stop("SIGBREAK"));
}
main().catch((err) => {
  console.error("[Server] 启动失败:", err);
  process.exit(1);
});
