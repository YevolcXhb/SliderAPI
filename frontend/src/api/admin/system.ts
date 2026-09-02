/**
 * System API endpoints for admin operations.
 *
 * 注意：检测更新 / 执行更新 / 回滚 / 重启服务功能已按要求移除，
 * 仅保留读取当前版本号（供侧栏静态展示）。
 */

import { apiClient } from '../client'

/**
 * Get current version
 */
export async function getVersion(): Promise<{ version: string }> {
  const { data } = await apiClient.get<{ version: string }>('/admin/system/version')
  return data
}

export const systemAPI = {
  getVersion
}

export default systemAPI
