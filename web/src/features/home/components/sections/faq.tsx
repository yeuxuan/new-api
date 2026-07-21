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
import { useTranslation } from 'react-i18next'

import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'

const FAQS = [
  {
    question: 'What is a multi-protocol AI API gateway?',
    answer:
      'It provides one managed access layer while preserving the request formats applications expect, including Anthropic, OpenAI-compatible, Gemini, and Responses-style endpoints.',
  },
  {
    question: 'Can I connect Claude Code?',
    answer:
      'Yes. Use the supported Anthropic base URL and token environment variables. The Claude Code API guide includes the setup and troubleshooting details.',
  },
  {
    question: 'How do I compare AI model prices?',
    answer:
      'Open the model catalog to compare input, output, cache, or per-request prices. Use the same token unit and account group for a meaningful comparison.',
  },
  {
    question: 'What does model health show?',
    answer:
      'Model pages summarize recent gateway observations such as latency, throughput, and success rate. They provide useful context, not a guarantee of future performance.',
  },
] as const

export function FAQ() {
  const { t } = useTranslation()

  return (
    <section className='px-6 py-20 md:py-28'>
      <div className='mx-auto grid max-w-5xl gap-10 lg:grid-cols-[0.75fr_1.25fr]'>
        <div>
          <p className='text-primary text-sm font-semibold'>
            {t('Gateway FAQ')}
          </p>
          <h2 className='mt-3 text-3xl font-bold tracking-tight'>
            {t('Questions developers ask before connecting')}
          </h2>
          <p className='text-muted-foreground mt-4 leading-7'>
            {t('Need the Claude Code setup?')}{' '}
            <Link
              to='/claude-code-api'
              className='text-foreground underline underline-offset-4'
            >
              {t('Read the complete Claude Code API guide.')}
            </Link>
          </p>
        </div>
        <Accordion>
          {FAQS.map((faq) => (
            <AccordionItem key={faq.question} value={faq.question}>
              <AccordionTrigger className='py-4 text-base'>
                {t(faq.question)}
              </AccordionTrigger>
              <AccordionContent className='text-muted-foreground pb-5 leading-7'>
                {t(faq.answer)}
              </AccordionContent>
            </AccordionItem>
          ))}
        </Accordion>
      </div>
    </section>
  )
}
