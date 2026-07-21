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
import { useEffect } from 'react'

import { useSystemConfig } from '@/hooks/use-system-config'

interface SeoMetadataProps {
  pageTitle: string
  description: string
  canonicalPath: string
  alternates?: Record<string, string>
}

function getOrCreateMeta(selector: string, attributes: Record<string, string>) {
  const existing = document.head.querySelector<HTMLMetaElement>(selector)
  if (existing) return existing

  const element = document.createElement('meta')
  for (const [name, value] of Object.entries(attributes)) {
    element.setAttribute(name, value)
  }
  document.head.appendChild(element)
  return element
}

function getOrCreateLink(selector: string, attributes: Record<string, string>) {
  const existing = document.head.querySelector<HTMLLinkElement>(selector)
  if (existing) return existing

  const element = document.createElement('link')
  for (const [name, value] of Object.entries(attributes)) {
    element.setAttribute(name, value)
  }
  document.head.appendChild(element)
  return element
}

export function SeoMetadata(props: SeoMetadataProps) {
  const { systemName } = useSystemConfig()

  useEffect(() => {
    const siteName = systemName?.trim() || 'New API'
    const title =
      siteName === 'New API'
        ? `${siteName} — ${props.pageTitle}`
        : `${siteName} — ${props.pageTitle} | New API`
    const canonical = new URL(props.canonicalPath, window.location.origin).href

    document.documentElement.dataset.seoManaged = 'true'
    document.title = title

    const values: [string, Record<string, string>, string][] = [
      ['meta[name="title"]', { name: 'title' }, title],
      ['meta[name="description"]', { name: 'description' }, props.description],
      ['meta[property="og:title"]', { property: 'og:title' }, title],
      [
        'meta[property="og:description"]',
        { property: 'og:description' },
        props.description,
      ],
      ['meta[property="og:url"]', { property: 'og:url' }, canonical],
      ['meta[name="twitter:title"]', { name: 'twitter:title' }, title],
      [
        'meta[name="twitter:description"]',
        { name: 'twitter:description' },
        props.description,
      ],
    ]
    for (const [selector, attributes, content] of values) {
      getOrCreateMeta(selector, attributes).content = content
    }
    getOrCreateMeta('meta[name="twitter:card"]', {
      name: 'twitter:card',
    }).content = 'summary'
    getOrCreateLink('link[rel="canonical"]', { rel: 'canonical' }).href =
      canonical

    document.head
      .querySelectorAll('link[rel="alternate"]')
      .forEach((element) => element.remove())
    for (const [language, path] of Object.entries(props.alternates ?? {})) {
      const alternate = document.createElement('link')
      alternate.rel = 'alternate'
      alternate.hreflang = language
      alternate.href = new URL(path, window.location.origin).href
      alternate.dataset.seoAlternate = 'true'
      document.head.appendChild(alternate)
    }
  }, [
    props.alternates,
    props.canonicalPath,
    props.description,
    props.pageTitle,
    systemName,
  ])

  return null
}
