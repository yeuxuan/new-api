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
import { getRouteApi } from '@tanstack/react-router'
import { Plus, Eraser, Download } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Button } from '@/components/ui/button'
import { api } from '@/lib/api'
import { downloadBlob } from '@/lib/download'

import { ClearCheckinQuotaDialog } from './dialogs/clear-checkin-quota-dialog'
import { useUsers } from './users-provider'

const route = getRouteApi('/_authenticated/users/')

export function UsersPrimaryButtons() {
  const { t } = useTranslation()
  const { setOpen, setCurrentRow, triggerRefresh } = useUsers()
  const search = route.useSearch() as { filter?: string; group?: string }
  const [clearCheckinOpen, setClearCheckinOpen] = useState(false)
  const [exportingUsers, setExportingUsers] = useState(false)
  const [exportingQuota, setExportingQuota] = useState(false)

  const handleCreate = () => {
    setCurrentRow(null)
    setOpen('create')
  }

  const handleExportUsers = async () => {
    setExportingUsers(true)
    try {
      const query = new URLSearchParams()
      if (search.filter) query.append('keyword', search.filter)
      if (search.group) query.append('group', search.group)
      const res = await api.get(`/api/user/export?${query.toString()}`, {
        responseType: 'blob',
      })
      downloadBlob(res.data, `users_export_${Date.now()}.csv`)
    } catch {
      toast.error(t('Export failed'))
    } finally {
      setExportingUsers(false)
    }
  }

  const handleExportQuotaLogs = async () => {
    setExportingQuota(true)
    try {
      const res = await api.get('/api/log/quota/export', {
        responseType: 'blob',
      })
      downloadBlob(res.data, `quota_logs_export_${Date.now()}.csv`)
    } catch {
      toast.error(t('Export failed'))
    } finally {
      setExportingQuota(false)
    }
  }

  return (
    <div className='flex gap-2'>
      <Button
        size='sm'
        variant='outline'
        onClick={handleExportUsers}
        disabled={exportingUsers}
      >
        <Download className='h-4 w-4' />
        {t('Export')}
      </Button>
      <Button
        size='sm'
        variant='outline'
        onClick={handleExportQuotaLogs}
        disabled={exportingQuota}
      >
        <Download className='h-4 w-4' />
        {t('Export Quota Details')}
      </Button>
      <Button
        size='sm'
        variant='outline'
        onClick={() => setClearCheckinOpen(true)}
      >
        <Eraser className='h-4 w-4' />
        {t('Clear Check-in Quota')}
      </Button>
      <Button size='sm' onClick={handleCreate}>
        <Plus className='h-4 w-4' />
        {t('Add User')}
      </Button>

      <ClearCheckinQuotaDialog
        open={clearCheckinOpen}
        onOpenChange={setClearCheckinOpen}
        onSuccess={triggerRefresh}
      />
    </div>
  )
}
