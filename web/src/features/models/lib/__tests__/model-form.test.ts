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

import type { Model } from '../../types'
import {
  modelFormSchema,
  transformFormDataToModelPayload,
  transformModelToFormDefaults,
} from '../model-form'

const MODEL_FIXTURE: Model = {
  id: 1,
  model_name: 'documented-model',
  api_document: '# API guide\n\nCall `POST /v1/tasks`.',
  status: 1,
  sync_official: 0,
  created_time: 1,
  updated_time: 1,
  name_rule: 0,
}

describe('model API documentation form', () => {
  test('loads and submits Markdown without altering it', () => {
    const defaults = transformModelToFormDefaults(MODEL_FIXTURE)
    const payload = transformFormDataToModelPayload(defaults)

    assert.equal(defaults.api_document, MODEL_FIXTURE.api_document)
    assert.equal(payload.api_document, MODEL_FIXTURE.api_document)
  })

  test('defaults missing documentation to an editable empty string', () => {
    const parsed = modelFormSchema.parse({ model_name: 'plain-model' })

    assert.equal(parsed.api_document, '')
  })
})
