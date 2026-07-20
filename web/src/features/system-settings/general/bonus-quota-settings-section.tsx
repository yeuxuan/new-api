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
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { useForm, type Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { z } from 'zod'

import { MultiSelect } from '@/components/multi-select'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { getEnabledModels } from '@/features/channels/api'

import { SettingsForm } from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useUpdateOption } from '../hooks/use-update-option'

const schema = z.object({
  validityDays: z.coerce.number().int().min(0),
  allowedModels: z.array(z.string()),
})

type Values = z.infer<typeof schema>

export function BonusQuotaSettingsSection({
  defaultValues,
}: {
  defaultValues: {
    validityDays: number
    allowedModels: string[]
  }
}) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const { data: modelsResponse, isLoading: modelsLoading } = useQuery({
    queryKey: ['channel', 'models_enabled'],
    queryFn: getEnabledModels,
  })
  const models = modelsResponse?.data ?? []

  const form = useForm<Values>({
    resolver: zodResolver(schema) as unknown as Resolver<Values>,
    defaultValues: {
      validityDays: defaultValues.validityDays,
      allowedModels: defaultValues.allowedModels,
    },
  })

  const { isDirty, isSubmitting } = form.formState

  async function onSubmit(values: Values) {
    const updates: Array<{ key: string; value: string }> = []
    const allowedModelsStr = values.allowedModels.join(',')

    if (values.validityDays !== defaultValues.validityDays) {
      updates.push({
        key: 'bonus_quota_setting.validity_days',
        value: String(values.validityDays),
      })
    }

    const defaultModelsStr = defaultValues.allowedModels.join(',')
    if (allowedModelsStr !== defaultModelsStr) {
      updates.push({
        key: 'bonus_quota_setting.allowed_models',
        value: allowedModelsStr,
      })
    }

    if (updates.length === 0) {
      toast.info(t('No changes to save'))
      return
    }

    for (const update of updates) {
      await updateOption.mutateAsync(update)
    }

    form.reset(values)
  }

  return (
    <SettingsSection title={t('Bonus Quota Restrictions')}>
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)} autoComplete='off'>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending || isSubmitting}
            isSaveDisabled={!isDirty}
            saveLabel='Save bonus quota settings'
          />
          <FormField
            control={form.control}
            name='validityDays'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Bonus quota validity (days)')}</FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={0}
                    placeholder={t('30')}
                    {...field}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Number of days bonus quota remains valid after being awarded. 0 means never expires. Applies to check-in and email bind rewards.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='allowedModels'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Bonus quota allowed models')}</FormLabel>
                <FormControl>
                  <MultiSelect
                    options={models.map((m) => ({ label: m, value: m }))}
                    selected={field.value}
                    onChange={field.onChange}
                    placeholder={t('Select models or add custom ones')}
                    allowCreate
                    createLabel='Add custom model "{{value}}"'
                    disabled={modelsLoading}
                    maxVisibleChips={8}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Bonus quota can only be used for selected models. Leave empty to allow all models.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
