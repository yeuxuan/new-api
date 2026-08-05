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
import assert from 'node:assert/strict'
import { describe, test } from 'node:test'

import { QueryClient } from '@tanstack/react-query'

import { invalidateModelCatalogQueries, modelsQueryKeys } from '../query-keys'

describe('model catalog query invalidation', () => {
  test('invalidates admin model data and all pricing document caches', () => {
    const queryClient = new QueryClient()
    const modelListKey = modelsQueryKeys.list({ p: 1 })
    const modelDetailKey = modelsQueryKeys.detail(7)
    const pricingListKey = ['pricing'] as const
    const modelDocumentKey = [
      'pricing',
      'model-api-document',
      'seedance-2.0-pro',
    ] as const
    const unrelatedKey = ['users'] as const

    for (const key of [
      modelListKey,
      modelDetailKey,
      pricingListKey,
      modelDocumentKey,
      unrelatedKey,
    ]) {
      queryClient.setQueryData(key, { loaded: true })
    }

    invalidateModelCatalogQueries(queryClient, 7)

    assert.equal(queryClient.getQueryState(modelListKey)?.isInvalidated, true)
    assert.equal(queryClient.getQueryState(modelDetailKey)?.isInvalidated, true)
    assert.equal(queryClient.getQueryState(pricingListKey)?.isInvalidated, true)
    assert.equal(
      queryClient.getQueryState(modelDocumentKey)?.isInvalidated,
      true
    )
    assert.equal(queryClient.getQueryState(unrelatedKey)?.isInvalidated, false)
  })
})
