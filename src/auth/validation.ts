import { HttpError } from '../http';
export function characters(value: unknown, label: string, min: number, max: number): string {
  if (typeof value !== 'string' || /[\ud800-\udfff]/u.test(value)) throw new HttpError(400, `${label}格式错误`);
  const length = Array.from(value).length;
  if (length < min || length > max) throw new HttpError(400, `${label}需要 ${min}–${max} 个字符`);
  return value;
}
export function accountValue(value: unknown): string { return characters(value, '账号', 8, 64); }
export function passwordValue(value: unknown): string { return characters(value, '密码', 10, 128); }
export function profileValues(body: Record<string, unknown>) {
  if (typeof body.name !== 'string') throw new HttpError(400, '请填写昵称');
  const name = characters(body.name.trim(), '昵称', 1, 64);
  const email = characters(body.email ?? '', '邮箱', 0, 254).trim();
  const phone = characters(body.phone ?? '', '手机号', 0, 32).trim();
  if (email && !/^[^\s@]+@[^\s@]+\.[^\s@]+$/.test(email)) throw new HttpError(400, '请输入有效的邮箱地址');
  if (phone && !/^\+?[0-9 ()-]{5,32}$/.test(phone)) throw new HttpError(400, '请输入有效的手机号');
  return { name, email, phone };
}
