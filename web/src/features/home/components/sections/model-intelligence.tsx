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
import { Link } from '@tanstack/react-router'
import { Activity, ArrowRight, Braces, CircleDollarSign } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

const SIGNALS = [
  {
    icon: Braces,
    title: 'Multi-protocol model access',
    description:
      'Use Anthropic, OpenAI-compatible, Gemini, and Responses-style endpoints from one gateway.',
  },
  {
    icon: CircleDollarSign,
    title: 'Live model pricing',
    description:
      'Compare input, output, cache, and per-request prices with the billing context that matters.',
  },
  {
    icon: Activity,
    title: 'Model health visibility',
    description:
      'Review recent latency, throughput, and success rate before choosing a model for real work.',
  },
] as const

export function ModelIntelligence() {
  const { t } = useTranslation()

  return (
    <section className='border-border/40 border-y px-6 py-20 md:py-28'>
      <div className='mx-auto max-w-6xl'>
        <div className='grid gap-10 lg:grid-cols-[0.9fr_1.3fr] lg:items-end'>
          <div>
            <p className='text-primary text-sm font-semibold'>
              {t('Live model intelligence')}
            </p>
            <h2 className='mt-3 text-3xl font-bold tracking-tight text-balance md:text-4xl'>
              {t('Price, protocol, and health in one decision surface')}
            </h2>
          </div>
          <div>
            <p className='text-muted-foreground max-w-2xl leading-7'>
              {t(
                'A model name alone is not enough. Compare current cost and recent operational signals, then open a model page for supported endpoints and capabilities.'
              )}
            </p>
            <Button
              className='mt-5'
              variant='outline'
              render={<Link to='/pricing' />}
            >
              {t('Explore model pricing and health')}
              <ArrowRight aria-hidden='true' />
            </Button>
          </div>
        </div>

        <div className='mt-12 grid gap-4 md:grid-cols-3'>
          {SIGNALS.map((signal) => (
            <article
              key={signal.title}
              className='bg-card rounded-2xl border p-6'
            >
              <signal.icon className='text-primary size-6' aria-hidden='true' />
              <h3 className='mt-5 font-semibold'>{t(signal.title)}</h3>
              <p className='text-muted-foreground mt-2 text-sm leading-6'>
                {t(signal.description)}
              </p>
            </article>
          ))}
        </div>
      </div>
    </section>
  )
}
