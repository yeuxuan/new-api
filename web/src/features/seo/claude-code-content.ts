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
export const CLAUDE_CODE_SECTIONS = [
  {
    title: 'What a Claude Code API proxy should solve',
    paragraphs: [
      'Claude Code is most useful when model access feels dependable rather than mysterious. A good Claude Code API proxy gives your terminal one stable endpoint while the gateway handles credentials, model availability, usage accounting, and upstream changes behind it. That separation matters for individual developers and even more for teams: local configuration stays small, keys can be rotated centrally, and switching a model or provider does not require every engineer to rebuild a personal setup. The result is a cleaner development workflow with fewer hidden dependencies on a single upstream account.',
      'The proxy should also expose enough operational information to support a real decision. A long model list is not useful by itself. Developers need to know which API protocol a model accepts, what input and output cost, whether prompt caching is priced differently, and how the model has behaved recently. Compute Token brings those signals together in the model catalog so that price, supported endpoints, latency, throughput, and success rate can be checked before a model is placed in a coding workflow.',
    ],
  },
  {
    title: 'Connect Claude Code with two environment variables',
    paragraphs: [
      'Claude Code supports a custom Anthropic base URL and an authentication token through environment variables. Set ANTHROPIC_BASE_URL to this gateway and ANTHROPIC_AUTH_TOKEN to a key created in your account. This keeps the official Claude Code client while changing only the API destination. You do not need to patch the client, run an unofficial binary, or place a second proxy on your laptop. The same configuration works in a shell profile, a temporary terminal session, a container, or a controlled team development image.',
      'Treat the key as a secret even when the setup is simple. Avoid committing it to a repository, pasting it into issue screenshots, or embedding it in a shared dotfile. For a team, create separate keys or policies for different people and environments so usage remains attributable. If a key is exposed, revoke it and issue a replacement. Centralized key management is one of the practical reasons to use an API gateway: it gives you a clear control point without changing the developer experience inside Claude Code.',
    ],
  },
  {
    title: 'Choose models with live pricing and health context',
    paragraphs: [
      'Coding model selection is a three-way tradeoff among quality, speed, and cost. A stronger model may be appropriate for architecture, difficult debugging, or a risky migration, while a faster or less expensive model can be better for routine edits, test generation, and repository exploration. The pricing catalog shows token-based or per-request billing in a consistent format. Where available, it also separates input, output, cached input, and cache creation costs, which prevents a low headline price from hiding the part of the workload that actually drives spend.',
      'Health signals add the time dimension that a static price table misses. Recent latency helps estimate how responsive an interactive coding session may feel. Throughput provides context for generation speed, and success rate shows whether requests have been completing reliably. These values describe recent gateway observations rather than a promise of future performance, but they are useful when comparing otherwise similar choices. Check them again before a long task or a production release instead of assuming yesterday’s fastest route is still the best route today.',
    ],
  },
  {
    title: 'Use one gateway across Anthropic and compatible APIs',
    paragraphs: [
      'The gateway is not limited to one client protocol. The public model catalog identifies support for Anthropic, OpenAI-compatible chat, Gemini, and OpenAI Responses-style endpoints when those routes are available for a model. That multi-protocol design lets a team keep Claude Code on the Anthropic interface while other applications use their native or compatible SDKs. You can standardize access, billing, and visibility without forcing every application into the exact same request shape or giving up features that depend on a particular protocol.',
      'Protocol compatibility should never be guessed from a model name. Two models with similar names can be exposed through different endpoint types, and a provider may support streaming, tools, images, or caching differently. Open the model detail page to review its supported endpoints and capabilities, then use the matching example in your client. This small check avoids many common errors: sending an Anthropic payload to an OpenAI route, selecting a model that is not enabled for the current group, or expecting a feature the upstream model does not provide.',
    ],
  },
  {
    title: 'Understand pricing before a long coding session',
    paragraphs: [
      'Claude Code can read substantial repository context and may make several tool-assisted turns before a task is complete. That makes both input and output pricing relevant. Repeated context can also make cache pricing important when prompt caching is supported. Estimate cost from the model detail page using the same token unit and account group that you expect to use. For fixed-price models, review the per-request amount instead. A realistic estimate should include retries and follow-up turns rather than multiplying a single short prompt by a headline rate.',
      'Price is only one part of total engineering cost. A model that needs repeated correction can be more expensive than a higher-priced model that completes the task accurately, while a premium model used for every trivial change can waste budget. A practical strategy is to match model strength to task risk, verify the generated change with tests, and review actual usage after representative work. The gateway’s centralized accounting makes those comparisons easier because different protocols and models are measured in one operational system.',
    ],
  },
  {
    title: 'Troubleshoot the most common connection errors',
    paragraphs: [
      'Start with the smallest possible diagnostic path. Confirm the base URL uses HTTPS and does not contain an accidental extra path. Confirm the token is present in the same shell process that launches Claude Code, and check that the selected model appears in the current pricing catalog. A 401 response usually points to a missing, invalid, or revoked credential. A 403 response can indicate account or group policy. A 404 often means the route or model name does not match the endpoint, while a 429 normally means a rate or quota limit has been reached.',
      'For intermittent failures, compare the time of the error with current model health and retry only when the operation is safe to repeat. Do not blindly replay tool actions that may have already changed files or external systems. If one model is degraded, choose another model that supports the same protocol and required capabilities. Record the request time, endpoint type, model name, status code, and any request identifier before asking for support; those details are far more useful than a screenshot that only says the request failed.',
    ],
  },
  {
    title: 'Build a safer team workflow',
    paragraphs: [
      'For team adoption, define a small supported matrix instead of allowing every model for every task. Document one default model for everyday coding, one stronger option for difficult reasoning, and a fallback that uses the same protocol. Set budget and rate controls that match the environment, keep production and personal keys separate, and review usage by key or user. This creates predictable behavior without removing developer choice. The matrix can evolve as live price and health evidence changes, but changes should be deliberate and communicated.',
      'Keep verification outside the model. Claude Code can propose and apply changes, but the repository’s tests, type checks, security checks, and code review remain the source of truth. Use scoped credentials for any external tools the agent can call, and avoid exposing production secrets in prompts or terminal output. An API gateway improves control and observability at the model boundary; it does not replace sound software delivery practices. The strongest setup combines reliable model access with clear permissions and deterministic validation.',
    ],
  },
  {
    title: 'Why this page belongs next to the live catalog',
    paragraphs: [
      'Many Claude Code API pages stop at a copied configuration snippet. Configuration is necessary, but it is not the whole user need. A developer arriving from search also needs to compare models, understand the billing unit, verify protocol support, evaluate recent health, and know what an error code means. This guide links those decisions directly to live catalog pages instead of duplicating a price table that becomes stale. The explanatory content stays stable while the model data can change with the service.',
      'That combination is the core of Compute Token’s model gateway: a practical setup path plus current operational context. Use this page to connect Claude Code, use the model catalog to select and compare, and use each model detail page to inspect price and health before committing to a workload. When new models or endpoint types become available, the catalog remains the authoritative list. This makes the landing page useful to a person today and maintainable for the site over time.',
    ],
  },
] as const

export const CLAUDE_CODE_FAQS = [
  {
    question: 'Can I use my existing Claude Code installation?',
    answer:
      'Yes. The setup keeps the official Claude Code client and points it to the gateway with supported environment variables. No patched client is required.',
  },
  {
    question: 'Is this only an Anthropic-compatible API?',
    answer:
      'No. The gateway can expose Anthropic, OpenAI-compatible, Gemini, and Responses-style endpoints. Check each model page because protocol support differs by model.',
  },
  {
    question: 'How do I find the cheapest Claude model?',
    answer:
      'Open the pricing catalog, filter for Claude models, and compare input, output, and cache prices using the same token unit and account group. Include recent health and task quality in the decision.',
  },
  {
    question: 'What should I check when Claude Code returns an error?',
    answer:
      'Verify the base URL, token, model name, endpoint type, quota, and current health. Keep the status code and request time so the failure can be diagnosed accurately.',
  },
] as const
