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
import type { SupportedParameter } from './mock-stats'

export type VideoSampleLanguage =
  | 'curl'
  | 'python'
  | 'typescript'
  | 'javascript'

type VideoModelDocumentation = {
  body: Record<string, unknown>
  parameters: SupportedParameter[]
  pollingSeconds: number
}

const MODEL_PARAMETER: SupportedParameter = {
  name: 'model',
  type: 'string',
  required: true,
  descriptionKey: 'Seedance model identifier',
}

const PROMPT_PARAMETER: SupportedParameter = {
  name: 'prompt',
  type: 'string',
  required: true,
  descriptionKey: 'Text prompt describing the desired video',
}

const DURATION_PARAMETER = (
  name: 'duration' | 'duration_sec',
  range: string,
  defaultValue?: number,
  enumValues?: string[]
): SupportedParameter => ({
  name,
  type: 'integer',
  ...(defaultValue === undefined ? {} : { defaultValue }),
  ...(enumValues ? { enumValues } : { range }),
  descriptionKey: 'Video duration in seconds',
})

const RATIO_PARAMETER = (
  name: 'ratio' | 'aspect_ratio',
  enumValues: string[],
  defaultValue: string
): SupportedParameter => ({
  name,
  type: 'enum',
  enumValues,
  defaultValue,
  descriptionKey: 'Output video aspect ratio',
})

const RESOLUTION_PARAMETER = (
  enumValues: string[],
  defaultValue: string
): SupportedParameter => ({
  name: 'resolution',
  type: 'enum',
  enumValues,
  defaultValue,
  descriptionKey: 'Output video resolution',
})

const STABLE_PARAMETERS: SupportedParameter[] = [
  MODEL_PARAMETER,
  PROMPT_PARAMETER,
  {
    name: 'mode_type',
    type: 'enum',
    enumValues: ['text2video', 'image2video', 'mixed2video'],
    defaultValue: 'text2video',
    descriptionKey: 'Generation mode selected for the supplied references',
  },
  DURATION_PARAMETER('duration', '4 ~ 15', 5),
  RATIO_PARAMETER(
    'ratio',
    ['adaptive', '16:9', '4:3', '1:1', '3:4', '9:16', '21:9'],
    'adaptive'
  ),
  RESOLUTION_PARAMETER(['480p', '720p'], '720p'),
  {
    name: 'enable_sound',
    type: 'enum',
    enumValues: ['on', 'off'],
    defaultValue: 'off',
    descriptionKey: 'Whether the generated video includes sound',
  },
  {
    name: 'image_urls',
    type: 'array',
    range: '0 ~ 9',
    descriptionKey: 'Public image URLs used by image-to-video mode',
  },
  {
    name: 'audio_urls',
    type: 'array',
    range: '0 ~ 3',
    descriptionKey: 'Public audio URLs used as references',
  },
  {
    name: 'mixed_list',
    type: 'array',
    range: '0 ~ 15',
    descriptionKey: 'Mixed image and audio references with url and type fields',
  },
]

const V2_PARAMETERS: SupportedParameter[] = [
  MODEL_PARAMETER,
  PROMPT_PARAMETER,
  DURATION_PARAMETER('duration', '4 ~ 15', 5),
  RATIO_PARAMETER('ratio', ['16:9', '9:16'], '16:9'),
  RESOLUTION_PARAMETER(['720P', '1080P'], '720P'),
  {
    name: 'images',
    type: 'array',
    range: '0 ~ 9',
    descriptionKey: 'Public image URLs used as references',
  },
  {
    name: 'audio_urls',
    type: 'array',
    range: '0 ~ 3',
    descriptionKey: 'Public audio URLs used as references',
  },
]

const PRO_720P_PARAMETERS: SupportedParameter[] = [
  MODEL_PARAMETER,
  {
    ...PROMPT_PARAMETER,
    range: '10 ~ 10000 chars',
  },
  DURATION_PARAMETER('duration_sec', '5 ~ 15', 5),
  RATIO_PARAMETER('aspect_ratio', ['9:16'], '9:16'),
  RESOLUTION_PARAMETER(['720p'], '720p'),
  {
    name: 'reference_url',
    type: 'string',
    descriptionKey: 'Single public HTTPS reference URL',
  },
  {
    name: 'reference_urls',
    type: 'array',
    range: '0 ~ 10',
    descriptionKey: 'Multiple public HTTPS reference URLs',
  },
  {
    name: 'enable_face_mask',
    type: 'boolean',
    defaultValue: false,
    descriptionKey: 'Whether faces in reference media are masked',
  },
]

const build431Parameters = (
  duration: SupportedParameter
): SupportedParameter[] => [
  MODEL_PARAMETER,
  { ...PROMPT_PARAMETER, range: '<= 5000 chars' },
  duration,
  RATIO_PARAMETER('ratio', ['16:9', '9:16', '1:1'], '16:9'),
  RESOLUTION_PARAMETER(['720p'], '720p'),
  {
    name: 'first_image',
    type: 'string',
    descriptionKey: 'Public image URL used as the first frame',
  },
  {
    name: 'last_image',
    type: 'string',
    descriptionKey: 'Public image URL used as the last frame',
  },
  {
    name: 'referenceImages',
    type: 'array',
    range: '0 ~ 4',
    descriptionKey: 'Public image URLs used as references',
  },
  {
    name: 'referenceVideos',
    type: 'array',
    range: '0 ~ 3',
    descriptionKey: 'Public video URLs used as references',
  },
  {
    name: 'referenceAudios',
    type: 'array',
    range: '0 ~ 1',
    descriptionKey: 'Public audio URLs used as references',
  },
]

const PARAMETERS_25: SupportedParameter[] = [
  MODEL_PARAMETER,
  PROMPT_PARAMETER,
  DURATION_PARAMETER('duration_sec', '>= 1', 5),
  RATIO_PARAMETER('ratio', ['16:9', '9:16', '4:3', '3:4'], '16:9'),
  RESOLUTION_PARAMETER(['480p', '720p'], '720p'),
  {
    name: 'input_images',
    type: 'array',
    range: '0 ~ 9',
    descriptionKey: 'Public input image URLs',
  },
  {
    name: 'input_audios',
    type: 'array',
    range: '0 ~ 3',
    descriptionKey: 'Public input audio URLs',
  },
]

const SAMPLE_PROMPT =
  'A cinematic tracking shot of a paper boat floating through a neon-lit rainy street.'

const SEEDANCE_DOCUMENTATION: Record<string, VideoModelDocumentation> = {
  '[V2]seedance-2.0': {
    body: {
      model: '[V2]seedance-2.0',
      prompt: SAMPLE_PROMPT,
      duration: 5,
      ratio: '16:9',
      resolution: '720P',
    },
    parameters: V2_PARAMETERS,
    pollingSeconds: 10,
  },
  'seedance-2.0-mini': stableDocumentation('seedance-2.0-mini'),
  'seedance-2.0-fast': stableDocumentation('seedance-2.0-fast'),
  'seedance-2.0-pro': stableDocumentation('seedance-2.0-pro'),
  'seedance-2.0-pro-720p': {
    body: {
      model: 'seedance-2.0-pro-720p',
      prompt: SAMPLE_PROMPT,
      duration_sec: 5,
      aspect_ratio: '9:16',
      resolution: '720p',
    },
    parameters: PRO_720P_PARAMETERS,
    pollingSeconds: 10,
  },
  'seedance-2.0-fast(431)': {
    body: {
      model: 'seedance-2.0-fast(431)',
      prompt: SAMPLE_PROMPT,
      duration: 10,
      ratio: '16:9',
      resolution: '720p',
    },
    parameters: build431Parameters(
      DURATION_PARAMETER('duration', '', 10, ['10', '15'])
    ),
    pollingSeconds: 30,
  },
  'seedance-2.0-pro(431)': {
    body: {
      model: 'seedance-2.0-pro(431)',
      prompt: SAMPLE_PROMPT,
      duration: 5,
      ratio: '16:9',
      resolution: '720p',
    },
    parameters: build431Parameters(DURATION_PARAMETER('duration', '4 ~ 15', 5)),
    pollingSeconds: 30,
  },
  'seedance-2.5': {
    body: {
      model: 'seedance-2.5',
      prompt: SAMPLE_PROMPT,
      duration_sec: 5,
      ratio: '16:9',
      resolution: '720p',
    },
    parameters: PARAMETERS_25,
    pollingSeconds: 10,
  },
}

function stableDocumentation(model: string): VideoModelDocumentation {
  return {
    body: {
      model,
      prompt: SAMPLE_PROMPT,
      mode_type: 'text2video',
      duration: 5,
      ratio: 'adaptive',
      resolution: '720p',
      enable_sound: 'off',
    },
    parameters: STABLE_PARAMETERS,
    pollingSeconds: 10,
  }
}

export function getSeedanceVideoDocumentation(
  modelName: string
): VideoModelDocumentation | undefined {
  return SEEDANCE_DOCUMENTATION[modelName]
}

export function buildVideoTaskSample(
  lang: VideoSampleLanguage,
  baseUrl: string,
  apiKeyEnv: string,
  modelName: string,
  endpointPath: string
): string {
  const documentation = getSeedanceVideoDocumentation(modelName)
  const body = documentation?.body ?? {
    model: modelName,
    prompt: SAMPLE_PROMPT,
    duration: 5,
  }
  const pollingSeconds = documentation?.pollingSeconds ?? 10
  const createUrl = `${baseUrl}${endpointPath}`
  const taskUrl = `${baseUrl}/v1/tasks`
  const contentUrl = `${baseUrl}/v1/videos`

  if (lang === 'curl') {
    const bodyJson = JSON.stringify(body, null, 2)
    return [
      `TASK_RESPONSE=$(curl -sS '${createUrl}' \\`,
      `  -H "Authorization: Bearer $${apiKeyEnv}" \\`,
      `  -H 'Content-Type: application/json' \\`,
      `  -d '${bodyJson.replaceAll('\n', '\n     ')}')`,
      `TASK_ID=$(printf '%s' "$TASK_RESPONSE" | jq -r '.task_id')`,
      '',
      'while true; do',
      `  TASK=$(curl -sS '${taskUrl}/'"$TASK_ID" \\`,
      `    -H "Authorization: Bearer $${apiKeyEnv}")`,
      `  STATUS=$(printf '%s' "$TASK" | jq -r '.status' | tr '[:upper:]' '[:lower:]')`,
      `  [ "$STATUS" = 'completed' ] || [ "$STATUS" = 'success' ] && break`,
      `  [ "$STATUS" = 'failed' ] || [ "$STATUS" = 'failure' ] && exit 1`,
      `  sleep ${pollingSeconds}`,
      'done',
      '',
      `curl -L '${contentUrl}/'"$TASK_ID"'/content' \\`,
      `  -H "Authorization: Bearer $${apiKeyEnv}" \\`,
      `  -o result.mp4`,
    ].join('\n')
  }

  if (lang === 'python') {
    const bodyJson = JSON.stringify(body, null, 2)
    return [
      'import json',
      'import time',
      'import requests',
      '',
      `base_url = "${baseUrl}"`,
      'headers = {',
      '    "Authorization": "Bearer <YOUR_API_KEY>",',
      '    "Content-Type": "application/json",',
      '}',
      `payload = json.loads(r'''${bodyJson}''')`,
      '',
      `task = requests.post(f"{base_url}${endpointPath}", headers=headers, json=payload)`,
      'task.raise_for_status()',
      'task_id = task.json()["task_id"]',
      '',
      'while True:',
      '    task = requests.get(f"{base_url}/v1/tasks/{task_id}", headers=headers)',
      '    task.raise_for_status()',
      '    status = task.json()["status"].lower()',
      '    if status in {"completed", "success"}:',
      '        break',
      '    if status in {"failed", "failure"}:',
      '        raise RuntimeError(task.json())',
      `    time.sleep(${pollingSeconds})`,
      '',
      'video = requests.get(',
      '    f"{base_url}/v1/videos/{task_id}/content",',
      '    headers=headers,',
      ')',
      'video.raise_for_status()',
      'with open("result.mp4", "wb") as file:',
      '    file.write(video.content)',
    ].join('\n')
  }

  const typed = lang === 'typescript'
  const envAccess = `process.env.${apiKeyEnv}`
  const taskType = typed
    ? ' as { task_id: string; status?: string; error?: unknown }'
    : ''
  return [
    `import { writeFile } from 'node:fs/promises'`,
    '',
    `const baseUrl = '${baseUrl}'`,
    `const headers = {`,
    `  Authorization: \`Bearer \${${envAccess}}\`,`,
    `  'Content-Type': 'application/json',`,
    `}`,
    '',
    `const createResponse = await fetch(\`${'${baseUrl}'}${endpointPath}\`, {`,
    `  method: 'POST',`,
    `  headers,`,
    `  body: JSON.stringify(${JSON.stringify(body, null, 2)}),`,
    `})`,
    `if (!createResponse.ok) throw new Error(await createResponse.text())`,
    `const created = (await createResponse.json())${taskType}`,
    `const taskId = created.task_id`,
    '',
    'while (true) {',
    `  const response = await fetch(\`${'${baseUrl}'}/v1/tasks/${'${taskId}'}\`, { headers })`,
    `  if (!response.ok) throw new Error(await response.text())`,
    `  const task = (await response.json())${taskType}`,
    `  const status = task.status?.toLowerCase()`,
    `  if (status === 'completed' || status === 'success') break`,
    `  if (status === 'failed' || status === 'failure') throw new Error(JSON.stringify(task))`,
    `  await new Promise((resolve) => setTimeout(resolve, ${pollingSeconds * 1000}))`,
    '}',
    '',
    `const video = await fetch(\`${'${baseUrl}'}/v1/videos/${'${taskId}'}/content\`, { headers })`,
    `if (!video.ok) throw new Error(await video.text())`,
    `const bytes = new Uint8Array(await video.arrayBuffer())`,
    `await writeFile('result.mp4', bytes)`,
  ].join('\n')
}
