import fs from 'node:fs';
import path from 'node:path';
import crypto from 'node:crypto';
import { HttpError } from './http';
export const STATIC_DIR = '/home/static';
export const UPLOAD_DIR = '/home/update';
export const TEMP_DIR = '/home/update/.incoming';
export function initializeStorage(): void {
  for (const dir of [STATIC_DIR, UPLOAD_DIR, TEMP_DIR]) {
    fs.mkdirSync(dir, { recursive: true, mode: 0o700 });
    if (fs.lstatSync(dir).isSymbolicLink() || fs.realpathSync(dir) !== dir) throw new Error('Storage directory must not be a symbolic link');
  }
}
export function safeDirectory(value: unknown): string {
  if (typeof value !== 'string' || value.length > 255 || !/^[A-Za-z0-9_-]{1,64}(\/[A-Za-z0-9_-]{1,64})*$/.test(value)) {
    throw new HttpError(400, 'path 只能包含字母、数字、短横线、下划线和分层斜杠');
  }
  return value;
}
export function storageDirectory(directory: string): string {
  safeDirectory(directory);
  let resolved = UPLOAD_DIR;
  for (const part of directory.split('/')) {
    resolved = path.join(resolved, part);
    try { fs.mkdirSync(resolved, { mode: 0o700 }); } catch (error) { if ((error as NodeJS.ErrnoException).code !== 'EEXIST') throw error; }
    if (fs.lstatSync(resolved).isSymbolicLink() || !fs.statSync(resolved).isDirectory() || fs.realpathSync(resolved) !== resolved) {
      throw new HttpError(400, '上传目录不合法');
    }
  }
  return resolved;
}
export async function removeTemp(filePath: string | undefined): Promise<void> {
  if (filePath) await fs.promises.unlink(filePath).catch(() => undefined);
}
export async function computeFileHash(filePath: string): Promise<string> {
  const hash = crypto.createHash('sha256');
  for await (const chunk of fs.createReadStream(filePath)) hash.update(chunk);
  return hash.digest('hex');
}
