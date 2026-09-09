import express from "express";
import cors from "cors";
import path from "path";
import fs from "fs";
import { initDatabase } from "./db";
import { uploadRouter } from "./routes/upload";
import { documentRouter } from "./routes/document";

const PORT = 8090;
export const STATIC_DIR = "/home/static";

async function main() {
  // Ensure static directory exists
  if (!fs.existsSync(STATIC_DIR)) {
    fs.mkdirSync(STATIC_DIR, { recursive: true });
  }

  // Initialize database
  await initDatabase();

  const app = express();

  app.use(cors());
  app.use(express.json());

  // Routes
  app.use("/api", uploadRouter);
  app.use("/api", documentRouter);

  // Health check
  app.get("/api/health", (_req, res) => {
    res.json({ status: "ok" });
  });

  app.listen(PORT, () => {
    console.log(`[Server] Running at http://localhost:${PORT}`);
  });
}

main().catch((err) => {
  console.error("Failed to start server:", err);
  process.exit(1);
});
