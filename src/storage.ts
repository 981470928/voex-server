import fs from "node:fs";
import path from "node:path";
import http from "node:http";
import https from "node:https";
import type { IncomingMessage, OutgoingHttpHeaders } from "node:http";
import { pipeline } from "node:stream/promises";
import type { Request, Response } from "express";
import { HttpError } from "./http";

const DESKTOP = path.join(process.env.USERPROFILE || process.env.HOME || "", "Desktop");
const UPLOAD_DIR = path.join(DESKTOP, "voex-upload");
export const TEMP_DIR = path.join(UPLOAD_DIR, ".incoming");
const STORAGE_URL = new URL(process.env.VOEX_STORAGE_URL || "http://127.0.0.1:8091");
const STORAGE_TIMEOUT_MS = 30_000;
if (!["http:", "https:"].includes(STORAGE_URL.protocol)) {
  throw new Error("VOEX_STORAGE_URL must use http or https");
}

export function initializeStorage(): void {
  for (const dir of [UPLOAD_DIR, TEMP_DIR]) {
    fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
    if (fs.lstatSync(dir).isSymbolicLink() || fs.realpathSync(dir) !== dir) {
      throw new Error("Storage directory must not be a symbolic link");
    }
  }
}

export function safeDirectory(value: unknown): string {
  if (typeof value !== "string" || value.length > 255 || !/^[A-Za-z0-9_-]{1,64}(\/[A-Za-z0-9_-]{1,64})*$/.test(value)) {
    throw new HttpError(400, "path 只能包含字母、数字、短横线、下划线和分层斜杠");
  }
  return value;
}

export async function removeTemp(filePath: string | undefined): Promise<void> {
  if (filePath) await fs.promises.unlink(filePath).catch(() => undefined);
}

function unavailable(): HttpError {
  return new HttpError(503, "文件存储服务暂不可用");
}

function storageRequest(method: string, pathname: string, headers: OutgoingHttpHeaders = {}) {
  const url = new URL(pathname, STORAGE_URL);
  const request = (url.protocol === "https:" ? https : http).request(url, { method, headers });
  const response = new Promise<IncomingMessage>((resolve, reject) => {
    request.once("response", (message) => {
      message.setTimeout(STORAGE_TIMEOUT_MS, () => message.destroy(unavailable()));
      resolve(message);
    });
    request.once("error", reject);
  });
  request.setTimeout(STORAGE_TIMEOUT_MS, () => request.destroy(unavailable()));
  return { request, response };
}

export async function storeFile(tempPath: string): Promise<string> {
  const info = await fs.promises.stat(tempPath);
  const { request, response } = storageRequest("POST", "/files", {
    "Content-Type": "application/octet-stream",
    "Content-Length": info.size,
  });
  const source = fs.createReadStream(tempPath);
  let upstream: IncomingMessage | undefined;
  const storedHash = response.then(async (message) => {
    upstream = message;
    const status = message.statusCode ?? 502;
    if (status < 200 || status >= 300) {
      message.resume();
      if (status === 413) throw new HttpError(413, "上传文件超过存储大小限制");
      if (status === 503) throw unavailable();
      throw new HttpError(502, "文件存储服务写入失败");
    }
    const chunks: Buffer[] = [];
    let size = 0;
    for await (const chunk of message) {
      const bytes = Buffer.isBuffer(chunk) ? chunk : Buffer.from(chunk);
      size += bytes.length;
      if (size > 16 * 1024) throw new HttpError(502, "文件存储服务返回格式错误");
      chunks.push(bytes);
    }
    let payload: unknown;
    try {
      payload = JSON.parse(Buffer.concat(chunks).toString("utf8"));
    } catch {
      throw new HttpError(502, "文件存储服务返回格式错误");
    }
    const hash = payload && typeof payload === "object" && "hash" in payload ? payload.hash : undefined;
    if (typeof hash !== "string" || !/^[a-f0-9]{64}$/.test(hash)) {
      throw new HttpError(502, "文件存储服务返回标识错误");
    }
    return hash;
  });
  try {
    const [, hash] = await Promise.all([pipeline(source, request), storedHash]);
    return hash;
  } catch (error) {
    throw error instanceof HttpError ? error : unavailable();
  } finally {
    source.destroy();
    request.destroy();
    upstream?.destroy();
  }
}

export async function streamStoredFile(hash: string, req: Request, res: Response): Promise<void> {
  if (!/^[a-f0-9]{64}$/.test(hash)) throw new HttpError(404, "文件不存在");
  const headers: OutgoingHttpHeaders = {};
  for (const name of ["range", "if-range"] as const) {
    const value = req.headers[name];
    if (typeof value === "string") headers[name] = value;
  }
  const { request, response } = storageRequest("GET", `/files/${hash}`, headers);
  let upstream: IncomingMessage | undefined;
  const cancel = () => {
    if (!res.writableFinished) {
      request.destroy();
      upstream?.destroy();
    }
  };
  req.once("aborted", cancel);
  res.once("close", cancel);
  request.end();
  try {
    upstream = await response;
    if (req.aborted || res.destroyed) return;
    const status = upstream.statusCode ?? 502;
    if (![200, 206, 416].includes(status)) {
      if (status === 404) throw new HttpError(404, "文件不存在");
      if (status === 503) throw unavailable();
      throw new HttpError(502, "文件存储服务读取失败");
    }
    res.status(status);
    for (const name of ["content-length", "content-range", "accept-ranges", "etag", "last-modified"]) {
      const value = upstream.headers[name];
      if (value !== undefined) res.setHeader(name, value);
    }
    await pipeline(upstream, res);
  } catch (error) {
    if (req.aborted) return;
    if (!res.headersSent) {
      for (const name of ["Content-Type", "Content-Disposition", "Content-Length", "Content-Range", "Accept-Ranges", "ETag", "Last-Modified"]) {
        res.removeHeader(name);
      }
    }
    throw error instanceof HttpError ? error : unavailable();
  } finally {
    req.off("aborted", cancel);
    res.off("close", cancel);
    request.destroy();
    upstream?.destroy();
  }
}
