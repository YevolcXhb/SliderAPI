/**
 * Admin compliance acknowledgement API
 */
import { apiClient } from '../client'

export interface AdminComplianceStatus {
  required: boolean
  version: string
  document_path_zh: string
  document_path_en: string
  document_url_zh: string
  document_url_en: string
  ack_phrase_zh: string
  ack_phrase_en: string
}

export async function getAdminComplianceStatus(): Promise<AdminComplianceStatus | null> {
  const { data } = await apiClient.get<AdminComplianceStatus>('/admin/compliance')
  return data || null
}

export async function acceptAdminCompliance(phrase: string): Promise<void> {
  await apiClient.post('/admin/compliance/accept', { phrase, language: 'zh' })
}

export const complianceAPI = {
  getStatus: getAdminComplianceStatus,
  accept: acceptAdminCompliance,
}

export default complianceAPI
