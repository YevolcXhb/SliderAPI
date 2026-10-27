/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { AlertTriangle } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { ConfirmDialog } from '@/components/confirm-dialog'
import { Checkbox } from '@/components/ui/checkbox'
import { handleServerError } from '@/lib/handle-server-error'

import { deletePlan } from '../../api'
import { useSubscriptions } from '../subscriptions-provider'

export function ForceDeletePlanDialog() {
  const { t } = useTranslation()
  const { open, setOpen, currentRow, triggerRefresh } = useSubscriptions()
  const [loading, setLoading] = useState(false)
  const [acknowledged, setAcknowledged] = useState(false)

  if (open !== 'force-delete' || !currentRow) return null

  const handleConfirm = async () => {
    setLoading(true)
    try {
      const res = await deletePlan(currentRow.plan.id, true)
      if (res.success) {
        toast.success(t('Deleted successfully'))
        triggerRefresh()
        setOpen(null)
      } else {
        handleServerError(res)
      }
    } catch (error) {
      handleServerError(error, t('Operation failed'))
    } finally {
      setLoading(false)
    }
  }

  return (
    <ConfirmDialog
      open
      onOpenChange={(v) => !v && setOpen(null)}
      title={
        <span className='text-destructive flex items-center gap-2'>
          <AlertTriangle className='size-4' />
          {t('Force delete plan')}
        </span>
      }
      desc={t(
        'Force deleting permanently removes the plan "{{title}}" and ALL of its user subscriptions, including active ones. Affected users are immediately downgraded from the plan group. This cannot be undone. Continue?',
        { title: currentRow.plan.title }
      )}
      handleConfirm={handleConfirm}
      isLoading={loading}
      disabled={!acknowledged}
      confirmText={t('Force delete')}
      destructive
    >
      <label className='flex items-start gap-2 rounded-md border border-destructive/40 bg-destructive/5 p-3 text-sm'>
        <Checkbox
          checked={acknowledged}
          onCheckedChange={(checked) => setAcknowledged(checked === true)}
          className='mt-0.5'
        />
        <span>
          {t(
            'I understand that active subscriptions will be terminated immediately and cannot be recovered.'
          )}
        </span>
      </label>
    </ConfirmDialog>
  )
}
