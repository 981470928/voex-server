import type { ErrorRequestHandler, Request, RequestHandler, Response } from "express";

export class HttpError extends Error {
  constructor(public readonly status: number, message: string) {
    super(message);
  }
}

export function asyncRoute(
  handler: (req: Request, res: Response) => Promise<void>,
): RequestHandler {
  return (req, res, next) => {
    void handler(req, res).catch(next);
  };
}

export function bodyObject(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) {
    throw new HttpError(400, "请求体必须是 JSON 对象");
  }
  return value as Record<string, unknown>;
}

export function nameValue(value: unknown, label = "名称"): string {
  if (typeof value !== "string" || !value.trim()) {
    throw new HttpError(400, `${label}必须是非空字符串`);
  }
  const name = value.trim();
  if (Array.from(name).length > 255) {
    throw new HttpError(400, `${label}不能超过 255 个字符`);
  }
  return name;
}

export function keyValue(value: unknown, label: string): string {
  if (typeof value !== "string" || !value.trim()) {
    throw new HttpError(400, `${label}必须是非空字符串`);
  }
  if (Array.from(value).length > 64 || value !== value.trim()) {
    throw new HttpError(400, `${label}不能超过 64 个字符或包含首尾空白`);
  }
  return value;
}

export const errorHandler: ErrorRequestHandler = (error: unknown, _req, res, next) => {
  if (res.headersSent) {
    next(error);
    return;
  }
  if (error instanceof HttpError) {
    res.status(error.status).json({ error: error.message });
    return;
  }
  if (error && typeof error === 'object' && 'code' in error && String(error.code).startsWith('LIMIT_')) {
    res.status(error.code === 'LIMIT_FILE_SIZE' ? 413 : 400).json({ error: error.code === 'LIMIT_FILE_SIZE' ? '上传文件超过大小限制' : '上传字段或文件数量不合法' });
    return;
  }
  const detail = error as { code?: string; type?: string; status?: number } | null;
  if (detail?.type === "entity.parse.failed" || detail?.status === 400) {
    res.status(400).json({ error: "请求格式错误，请检查 JSON 和路径参数" });
    return;
  }
  if (detail?.status === 413) {
    res.status(413).json({ error: "请求内容超过大小限制" });
    return;
  }
  if (detail?.code === "ER_DUP_ENTRY") {
    res.status(409).json({ error: "资源标识冲突，请重试" });
    return;
  }
  if (
    detail?.code === "ER_ROW_IS_REFERENCED_2" ||
    detail?.code === "ER_NO_REFERENCED_ROW_2"
  ) {
    res.status(409).json({ error: "资源关联已发生变化，请刷新后重试" });
    return;
  }
  if (detail?.code === "ER_LOCK_DEADLOCK" || detail?.code === "ER_LOCK_WAIT_TIMEOUT") {
    res.status(409).json({ error: "工作区正在更新，请稍后重试" });
    return;
  }
  if (detail?.code === "ER_DATA_TOO_LONG") {
    res.status(400).json({ error: "字段内容超过长度限制" });
    return;
  }
  // 不输出原始数据库异常，其中会包含 SQL、参数和连接信息。
  console.error("[API] 请求处理失败");
  res.status(500).json({ error: "服务器处理失败，请稍后重试" });
};
