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
import { describe, expect, test } from 'vitest'

import type { User } from '../../types'
import { transformFormDataToPayload, transformUserToFormDefaults } from '../user-form'

const baseUser: User = {
  id: 7,
  username: 'tester',
  display_name: 'Tester',
  quota: 0,
  used_quota: 0,
  request_count: 0,
  group: 'default',
  status: 1,
  role: 1,
}

describe('hide_pricing_info form round-trip', () => {
  test.each([
    ['flagged user setting', '{"hide_pricing_info":true}', true],
    ['setting without the flag', '{"language":"zh"}', false],
    ['missing setting', undefined, false],
    ['invalid setting JSON', 'not-json', false],
  ])('transformUserToFormDefaults reads %s', (_name, setting, expected) => {
    const form = transformUserToFormDefaults({ ...baseUser, setting })
    expect(form.hide_pricing_info).toBe(expected)
  })

  test('update payload always carries the flag so the backend can clear it', () => {
    const payload = transformFormDataToPayload(
      {
        username: 'tester',
        display_name: 'Tester',
        hide_pricing_info: false,
      },
      7
    )
    expect(payload.hide_pricing_info).toBe(false)
  })

  test('create payload omits the flag', () => {
    const payload = transformFormDataToPayload({
      username: 'tester',
      display_name: 'Tester',
      hide_pricing_info: true,
    })
    expect(payload.hide_pricing_info).toBeUndefined()
  })
})
