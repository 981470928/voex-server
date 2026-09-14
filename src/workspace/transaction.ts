import type { PoolConnection } from "mysql2/promise";
import { getPool } from "../db";
import { lockWorkspace } from "./store";

/** 读接口共享一致快照；写接口先取得工作区行锁，再读数据和检查权限。 */
export async function workspaceTransaction<T>(
  write: boolean,
  operation: (connection: PoolConnection) => Promise<T>,
): Promise<T> {
  const connection = await getPool().getConnection();
  try {
    await connection.query("SET TRANSACTION ISOLATION LEVEL REPEATABLE READ");
    await connection.beginTransaction();
    if (write) await lockWorkspace(connection);
    const value = await operation(connection);
    await connection.commit();
    return value;
  } catch (error) {
    try {
      await connection.rollback();
    } catch {
      // 丢弃状态不明的连接，保留原始业务异常。
      connection.destroy();
    }
    throw error;
  } finally {
    connection.release();
  }
}
