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

import type { PricingModel } from '../../types'
import { buildSupportedParameters } from '../mock-stats'
import {
  buildVideoTaskSample,
  getSeedanceVideoDocumentation,
} from '../video-api-docs'

const SEEDANCE_MODELS = [
  '[V2]seedance-2.0',
  'seedance-2.0-mini',
  'seedance-2.0-fast',
  'seedance-2.0-pro',
  'seedance-2.0-pro-720p',
  'seedance-2.0-fast(431)',
  'seedance-2.0-pro(431)',
  'seedance-2.5',
]

describe('Seedance API documentation', () => {
  test('documents all eight configured Seedance models', () => {
    for (const model of SEEDANCE_MODELS) {
      const documentation = getSeedanceVideoDocumentation(model)
      assert.ok(documentation, `${model} must have API documentation`)
      assert.equal(documentation.body.model, model)
      assert.equal(documentation.parameters[0]?.name, 'model')
      assert.equal(documentation.parameters[1]?.name, 'prompt')
    }
  })

  test('keeps each upstream request dialect visible in the parameter table', () => {
    const parameterNames = (model: string) =>
      getSeedanceVideoDocumentation(model)?.parameters.map((item) => item.name)

    assert.deepEqual(parameterNames('[V2]seedance-2.0'), [
      'model',
      'prompt',
      'duration',
      'ratio',
      'resolution',
      'images',
      'audio_urls',
    ])
    assert.ok(parameterNames('seedance-2.0-fast')?.includes('mode_type'))
    assert.ok(
      parameterNames('seedance-2.0-pro-720p')?.includes('reference_urls')
    )
    assert.ok(
      parameterNames('seedance-2.0-fast(431)')?.includes('referenceVideos')
    )
    assert.ok(parameterNames('seedance-2.5')?.includes('input_images'))

    const pricingModel: PricingModel = {
      id: 1,
      model_name: 'seedance-2.0-pro-720p',
      quota_type: 1,
      model_ratio: 0,
      completion_ratio: 1,
      enable_groups: ['seedance'],
      supported_endpoint_types: ['openai-video'],
    }
    assert.deepEqual(
      buildSupportedParameters(pricingModel).map((item) => item.name),
      parameterNames('seedance-2.0-pro-720p')
    )
  })

  test('generates runnable samples for the full asynchronous task workflow', () => {
    for (const language of [
      'curl',
      'python',
      'typescript',
      'javascript',
    ] as const) {
      const sample = buildVideoTaskSample(
        language,
        'https://api.example.com',
        'NEW_API_KEY',
        'seedance-2.0-pro-720p',
        '/v1/tasks'
      )

      assert.match(sample, /\/v1\/tasks/)
      assert.match(sample, /task_id|taskId/)
      assert.match(sample, /\bid\b/)
      assert.match(sample, /\/v1\/videos\/.*\/content/)
      assert.match(sample, /seedance-2\.0-pro-720p/)
      assert.doesNotMatch(sample, /messages|chat\/completions/)
    }
  })

  test('uses the longer polling interval required by 431 models', () => {
    const sample = buildVideoTaskSample(
      'python',
      'https://api.example.com',
      'NEW_API_KEY',
      'seedance-2.0-fast(431)',
      '/v1/tasks'
    )

    assert.match(sample, /time\.sleep\(30\)/)
    assert.equal(
      getSeedanceVideoDocumentation('seedance-2.0-fast(431)')?.body.duration,
      10
    )
  })
})
