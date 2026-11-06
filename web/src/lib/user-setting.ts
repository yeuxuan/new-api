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
type UserSettingLike = Record<string, unknown> | string | null | undefined

/**
 * Whether the backend flagged this user hide_pricing_info: group ratios and
 * the model square / rankings pages are hidden from them. The setting comes
 * from the auth user payload either as a JSON string or a parsed object.
 */
export function isPricingHidden(setting: UserSettingLike): boolean {
  if (!setting) return false
  let parsed: Record<string, unknown>
  if (typeof setting === 'string') {
    try {
      const value: unknown = JSON.parse(setting)
      if (!value || typeof value !== 'object') return false
      parsed = value as Record<string, unknown>
    } catch {
      return false
    }
  } else {
    parsed = setting
  }
  return parsed.hide_pricing_info === true
}
