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
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { PublicLayout } from '@/components/layout'
import { Footer } from '@/components/layout/components/footer'
import { SeoMetadata } from '@/components/seo-metadata'
import {
  Accordion,
  AccordionContent,
  AccordionItem,
  AccordionTrigger,
} from '@/components/ui/accordion'
import { Button } from '@/components/ui/button'

import { CLAUDE_CODE_FAQS, CLAUDE_CODE_SECTIONS } from './claude-code-content'

interface ClaudeCodeApiProps {
  language: 'en' | 'zhCN'
}

export function ClaudeCodeApi(props: ClaudeCodeApiProps) {
  const { i18n } = useTranslation()
  const t = useMemo(
    () => i18n.getFixedT(props.language),
    [i18n, props.language]
  )
  const isChinese = props.language === 'zhCN'
  const canonicalPath = isChinese ? '/zh/claude-code-api' : '/claude-code-api'
  const baseURL = typeof window === 'undefined' ? '' : window.location.origin
  const setup = `export ANTHROPIC_BASE_URL="${baseURL}"\nexport ANTHROPIC_AUTH_TOKEN="your-api-key"\nclaude`

  return (
    <PublicLayout showMainContainer={false}>
      <SeoMetadata
        pageTitle={t(
          isChinese
            ? 'Claude Code API proxy with live pricing and model health'
            : 'Claude Code API Proxy with Live Pricing'
        )}
        description={t(
          'Connect Claude Code to a multi-protocol API gateway, compare supported Claude model prices, and inspect live model health before you build.'
        )}
        canonicalPath={canonicalPath}
        alternates={{
          en: '/claude-code-api',
          'zh-CN': '/zh/claude-code-api',
          'x-default': '/claude-code-api',
        }}
      />

      <main lang={isChinese ? 'zh-CN' : 'en'}>
        <section className='border-border/50 relative overflow-hidden border-b px-6 pt-28 pb-20'>
          <div className='bg-primary/8 absolute top-[-10rem] left-1/2 -z-10 size-[36rem] -translate-x-1/2 rounded-full blur-3xl' />
          <div className='mx-auto max-w-4xl text-center'>
            <p className='text-primary mb-4 text-sm font-semibold tracking-wide uppercase'>
              {t('Multi-protocol coding model API')}
            </p>
            <h1 className='text-4xl font-bold tracking-tight text-balance sm:text-6xl'>
              {t('Claude Code API proxy with live pricing and model health')}
            </h1>
            <p className='text-muted-foreground mx-auto mt-6 max-w-3xl text-lg leading-8'>
              {t(
                'Connect the official Claude Code client through one stable gateway, then compare current model price, protocol support, latency, throughput, and success rate before you choose.'
              )}
            </p>
            <div className='mt-8 flex flex-wrap justify-center gap-3'>
              <Button size='lg' render={<Link to='/sign-up' />}>
                {t('Create an API key')}
                <ArrowRight aria-hidden='true' />
              </Button>
              <Button
                variant='outline'
                size='lg'
                render={<Link to='/pricing' />}
              >
                {t('Compare live model pricing')}
              </Button>
            </div>
          </div>
        </section>

        <section className='mx-auto max-w-5xl px-6 py-16'>
          <div className='grid gap-4 md:grid-cols-3'>
            {[
              [
                Braces,
                'Anthropic protocol',
                'Use the interface Claude Code expects without modifying the client.',
              ],
              [
                CircleDollarSign,
                'Live pricing context',
                'Compare input, output, cache, and per-request prices in one catalog.',
              ],
              [
                Activity,
                'Recent health signals',
                'Review latency, throughput, and success rate before a long task.',
              ],
            ].map(([Icon, title, description]) => {
              const FeatureIcon = Icon as typeof Braces
              return (
                <article
                  key={title as string}
                  className='bg-card rounded-2xl border p-6'
                >
                  <FeatureIcon
                    className='text-primary size-6'
                    aria-hidden='true'
                  />
                  <h2 className='mt-4 text-base font-semibold'>
                    {t(title as string)}
                  </h2>
                  <p className='text-muted-foreground mt-2 text-sm leading-6'>
                    {t(description as string)}
                  </p>
                </article>
              )
            })}
          </div>

          <div className='mt-14 grid gap-8 lg:grid-cols-[1fr_1.15fr] lg:items-start'>
            <div>
              <p className='text-primary text-sm font-semibold'>
                {t('Quick setup')}
              </p>
              <h2 className='mt-2 text-2xl font-bold tracking-tight'>
                {t('Point Claude Code at the gateway')}
              </h2>
              <p className='text-muted-foreground mt-4 leading-7'>
                {t(
                  'Create a key in your account, export the supported Anthropic environment variables, and start the official Claude Code client from the same shell.'
                )}
              </p>
            </div>
            <pre className='bg-foreground text-background overflow-x-auto rounded-2xl p-6 text-sm leading-7 shadow-xl'>
              <code>{setup}</code>
            </pre>
          </div>
        </section>

        <article className='mx-auto max-w-4xl px-6 pb-16'>
          {CLAUDE_CODE_SECTIONS.map((section) => (
            <section
              key={section.title}
              className='border-border/60 border-t py-10 first:border-t-0'
            >
              <h2 className='text-2xl font-bold tracking-tight'>
                {t(section.title)}
              </h2>
              <div className='text-muted-foreground mt-5 space-y-5 text-base leading-8'>
                {section.paragraphs.map((paragraph) => (
                  <p key={paragraph}>{t(paragraph)}</p>
                ))}
              </div>
            </section>
          ))}

          <section className='border-border/60 border-t pt-10'>
            <h2 className='text-2xl font-bold tracking-tight'>
              {t('Claude Code API questions')}
            </h2>
            <Accordion className='mt-6'>
              {CLAUDE_CODE_FAQS.map((faq) => (
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
          </section>

          <section className='bg-muted/40 mt-14 rounded-3xl border p-8 text-center sm:p-12'>
            <h2 className='text-2xl font-bold tracking-tight'>
              {t('Choose with current data, then build')}
            </h2>
            <p className='text-muted-foreground mx-auto mt-3 max-w-2xl leading-7'>
              {t(
                'Open the live catalog to compare Claude models, protocol support, pricing, and recent health in one place.'
              )}
            </p>
            <Button className='mt-6' size='lg' render={<Link to='/pricing' />}>
              {t('Explore model pricing and health')}
              <ArrowRight aria-hidden='true' />
            </Button>
          </section>
        </article>
      </main>
      <Footer />
    </PublicLayout>
  )
}
