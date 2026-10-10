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
export function subscriptionBalanceQuota(
  priceAmount: number,
  quotaPerUnit: number
): number {
  if (
    !Number.isFinite(priceAmount) ||
    priceAmount < 0 ||
    !Number.isFinite(quotaPerUnit) ||
    quotaPerUnit <= 0
  ) {
    return Number.POSITIVE_INFINITY
  }

  // Match the backend's decimal multiplication followed by Ceil, without
  // letting binary floating-point noise turn 0.07 * 100 into 8 quota units.
  let coefficient = 1n
  let exponent = 0
  for (const value of [priceAmount, quotaPerUnit]) {
    const [mantissa, power = '0'] = value.toString().split('e')
    const [integer, fraction = ''] = mantissa.split('.')
    coefficient *= BigInt(integer + fraction)
    exponent += Number(power) - fraction.length
  }
  const scale = 10n ** BigInt(Math.abs(exponent))
  const quota =
    exponent >= 0 ? coefficient * scale : (coefficient + scale - 1n) / scale
  return quota <= BigInt(Number.MAX_SAFE_INTEGER)
    ? Number(quota)
    : Number.POSITIVE_INFINITY
}
