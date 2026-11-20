import { mkdir, writeFile } from 'node:fs/promises'
import { dirname } from 'node:path'

import type { Page, Request, Response, TestInfo } from '@playwright/test'

export type GridStallPhase =
  | 'H47_NOT_COMPLETE'
  | 'H48_NOT_STARTED'
  | 'H48_STARTED_NO_RESPONSE'
  | 'H48_RESPONSE_RECEIVED_UI_NOT_COMMITTED'
  | 'UNCLASSIFIED'

export interface ViewConfigExpectation {
  filters?: Array<{ fieldId: string; op: string; value: unknown }>
  sorts?: Array<{ fieldId: string; dir: 'asc' | 'desc' }>
  group?: string | null
}

export interface ExpectedGridState {
  recordTitles: string[]
  groupLabels?: string[]
}

interface NetworkRecord {
  sequence: number
  kind: 'H47' | 'H48' | 'RECORD_WRITE'
  method: string
  url: string
  query: Record<string, string>
  startedAt: string
  startedMs: number
  endedAt?: string
  endedMs?: number
  durationMs?: number
  status?: number
  ok?: boolean
  requestBody?: unknown
  responseBody?: unknown
  failure?: string
}

interface PhaseState {
  label: string
  startedSequence: number
  expectedConfig?: ViewConfigExpectation
  expectedGrid?: ExpectedGridState
}

const EMPTY_CONFIG: ViewConfigExpectation = {
  filters: [],
  sorts: [],
  group: null,
}

function iso(timestamp: number): string {
  return new Date(timestamp).toISOString()
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === 'object' && value !== null && !Array.isArray(value)
}

function decodeCommonValue(raw: unknown): unknown {
  if (!isRecord(raw)) return null
  if ('string_value' in raw) return raw.string_value
  if ('bool_value' in raw) return raw.bool_value
  if ('int_value' in raw) {
    const value = Number(raw.int_value)
    return Number.isFinite(value) ? value : raw.int_value
  }
  if ('double_value' in raw) return raw.double_value
  if ('date_value_ms' in raw) {
    const value = Number(raw.date_value_ms)
    return Number.isFinite(value) ? value : raw.date_value_ms
  }
  if (isRecord(raw.list_value) && Array.isArray(raw.list_value.value_list)) {
    return raw.list_value.value_list.map(decodeCommonValue)
  }
  return raw
}

function normalizeOp(raw: unknown): string {
  return String(raw ?? '')
    .replace(/^VIEW_FILTER_OP_/, '')
    .toLowerCase()
}

function normalizeDir(raw: unknown): 'asc' | 'desc' {
  return String(raw ?? '')
    .replace(/^SORT_DIR_/, '')
    .toLowerCase() === 'desc'
    ? 'desc'
    : 'asc'
}

function rawViewExpectation(rawViewConfig: unknown): ViewConfigExpectation {
  if (!isRecord(rawViewConfig)) return EMPTY_CONFIG
  const filters = Array.isArray(rawViewConfig.filter_list)
    ? rawViewConfig.filter_list.flatMap((entry) => {
        if (!isRecord(entry) || typeof entry.field_id !== 'string') return []
        return [
          {
            fieldId: entry.field_id,
            op: normalizeOp(entry.op),
            value: decodeCommonValue(entry.value),
          },
        ]
      })
    : []
  const sorts = Array.isArray(rawViewConfig.sort_list)
    ? rawViewConfig.sort_list.flatMap((entry) => {
        if (!isRecord(entry) || typeof entry.field_id !== 'string') return []
        return [{ fieldId: entry.field_id, dir: normalizeDir(entry.dir) }]
      })
    : []
  const firstGroup =
    Array.isArray(rawViewConfig.group_by_list) && isRecord(rawViewConfig.group_by_list[0])
      ? rawViewConfig.group_by_list[0]
      : undefined
  return {
    filters,
    sorts,
    group:
      firstGroup !== undefined && typeof firstGroup.field_id === 'string'
        ? firstGroup.field_id
        : null,
  }
}

function expectationFromSignature(signature: string): ViewConfigExpectation {
  if (signature === '') return EMPTY_CONFIG
  try {
    const parsed = JSON.parse(signature) as {
      filters?: Array<[string, string, unknown]>
      sorts?: Array<[string, 'asc' | 'desc']>
      group?: [string, string | null] | null
    }
    return {
      filters: (parsed.filters ?? []).map(([fieldId, op, value]) => ({
        fieldId,
        op,
        value,
      })),
      sorts: (parsed.sorts ?? []).map(([fieldId, dir]) => ({ fieldId, dir })),
      group: parsed.group?.[0] ?? null,
    }
  } catch {
    return EMPTY_CONFIG
  }
}

export function viewDataSignature(expectation: ViewConfigExpectation): string {
  const filters = expectation.filters ?? []
  const sorts = expectation.sorts ?? []
  const group = expectation.group ?? null
  if (filters.length === 0 && sorts.length === 0 && group === null) return ''
  return JSON.stringify({
    filters: filters.map((filter) => [filter.fieldId, filter.op, filter.value]),
    sorts: sorts.map((sort) => [sort.fieldId, sort.dir]),
    group: group === null ? null : [group, null],
  })
}

function expectationsEqual(
  left: ViewConfigExpectation,
  right: ViewConfigExpectation,
): boolean {
  return viewDataSignature(left) === viewDataSignature(right)
}

function responseCode(body: unknown): number | null {
  return isRecord(body) && typeof body.code === 'number' ? body.code : null
}

function recordQueryKey(
  tableId: string,
  viewId: string,
  expectation: ViewConfigExpectation,
): unknown[] {
  const signature = viewDataSignature(expectation)
  const base = ['records', 'table', tableId, 'list', viewId]
  return signature === '' ? base : [...base, 'conditions', signature]
}

function asStringArray(value: unknown): string[] {
  return Array.isArray(value)
    ? value.filter((item): item is string => typeof item === 'string')
    : []
}

/**
 * Browser-side grid-stall probe. It observes only public network traffic and rendered UI;
 * no application module, private function, or call count is inspected.
 */
export class GridStallProbe {
  private readonly records = new Map<Request, NetworkRecord>()
  private readonly events: NetworkRecord[] = []
  private readonly bodyTasks: Promise<void>[] = []
  private sequence = 0
  private phase: PhaseState | null = null

  constructor(
    private readonly page: Page,
    private readonly ids: {
      baseId: string
      tableId: string
      viewId: string
      primaryFieldId: string
    },
  ) {
    page.on('request', (request) => this.onRequest(request))
    page.on('response', (response) => this.onResponse(response))
    page.on('requestfinished', (request) => this.onRequestFinished(request))
    page.on('requestfailed', (request) => this.onRequestFailed(request))
  }

  beginPhase(label: string, expectedConfig?: ViewConfigExpectation): void {
    this.phase = {
      label,
      startedSequence: this.sequence,
      expectedConfig,
    }
  }

  setExpectedGrid(expectedGrid: ExpectedGridState): void {
    if (this.phase !== null) this.phase.expectedGrid = expectedGrid
  }

  latestSaveMatches(expectation: ViewConfigExpectation): boolean {
    const save = this.latestSave()
    if (save === undefined || save.endedMs === undefined) return false
    if (save.status === undefined || save.status < 200 || save.status >= 300) return false
    if (responseCode(save.responseBody) !== 0) return false
    const saved = isRecord(save.responseBody)
      && isRecord(save.responseBody.data)
      ? save.responseBody.data.view_config
      : undefined
    return expectationsEqual(rawViewExpectation(saved), expectation)
  }

  private onRequest(request: Request): void {
    const kind = this.classifyRequest(request)
    if (kind === null) return
    const url = new URL(request.url())
    const startedMs = Date.now()
    let requestBody: unknown
    if (kind !== 'H48') {
      try {
        requestBody = request.postDataJSON()
      } catch {
        requestBody = request.postData() ?? undefined
      }
    }
    const record: NetworkRecord = {
      sequence: this.sequence,
      kind,
      method: request.method(),
      url: request.url(),
      query: Object.fromEntries(url.searchParams.entries()),
      startedAt: iso(startedMs),
      startedMs,
      requestBody,
    }
    this.sequence += 1
    this.records.set(request, record)
    this.events.push(record)
  }

  private onResponse(response: Response): void {
    const record = this.records.get(response.request())
    if (record === undefined) return
    const endedMs = Date.now()
    record.endedAt = iso(endedMs)
    record.endedMs = endedMs
    record.durationMs = endedMs - record.startedMs
    record.status = response.status()
    record.ok = response.ok()
    const bodyTask = response
      .json()
      .then((body) => {
        record.responseBody = body
      })
      .catch((failure: unknown) => {
        record.failure = `response body decode failed: ${
          failure instanceof Error ? failure.message : String(failure)
        }`
      })
    this.bodyTasks.push(bodyTask)
  }

  private onRequestFinished(request: Request): void {
    const record = this.records.get(request)
    if (record === undefined || record.endedMs !== undefined) return
    const endedMs = Date.now()
    record.endedAt = iso(endedMs)
    record.endedMs = endedMs
    record.durationMs = endedMs - record.startedMs
  }

  private onRequestFailed(request: Request): void {
    const record = this.records.get(request)
    if (record === undefined) return
    const endedMs = Date.now()
    record.endedAt = iso(endedMs)
    record.endedMs = endedMs
    record.durationMs = endedMs - record.startedMs
    record.failure = request.failure()?.errorText ?? 'request failed without error text'
  }

  private classifyRequest(request: Request): NetworkRecord['kind'] | null {
    const url = new URL(request.url())
    const recordsPath = `/api/v1/bases/${encodeURIComponent(this.ids.baseId)}/records`
    if (url.pathname === recordsPath) {
      if (request.method() === 'GET') {
        if (
          url.searchParams.get('table_id') === this.ids.tableId
          && url.searchParams.get('view_id') === this.ids.viewId
        ) {
          return 'H48'
        }
      }
      if (request.method() === 'POST') return 'RECORD_WRITE'
    }
    if (
      request.method() === 'PUT'
      && url.pathname === `/api/v1/views/${encodeURIComponent(this.ids.viewId)}/config`
    ) {
      return 'H47'
    }
    return null
  }

  private eventsAfterPhase(): NetworkRecord[] {
    if (this.phase === null) return this.events
    return this.events.filter((event) => event.sequence >= this.phase!.startedSequence)
  }

  private latestSave(): NetworkRecord | undefined {
    return this.eventsAfterPhase()
      .filter((event) => event.kind === 'H47')
      .at(-1)
  }

  private latestQueryAfterSave(save: NetworkRecord | undefined): NetworkRecord | undefined {
    return this.eventsAfterPhase()
      .filter((event) => event.kind === 'H48')
      .filter((event) => save === undefined || event.startedMs >= (save.endedMs ?? Infinity))
      .at(-1)
  }

  private async captureUiState(): Promise<{
    saveStatus: string | null
    recordWindowState: string | null
    ariaBusy: string | null
    scrollTop: number | null
    applyingViewConditions: boolean
    loadingRecords: boolean
    error: string | null
    recordTitles: string[]
    groupLabels: string[]
    groupKeys: string[]
    groupFieldValue: string | null
  }> {
    const grid = this.page.getByRole('grid', { name: '记录网格' })
    const titleCells = this.page.locator(
      `[data-testid^="cell-"][data-testid$="-${this.ids.primaryFieldId}"]`,
    )
    const groupButtons = this.page.locator('[data-group-key] button')
    const groupSelect = this.page.getByLabel('分组字段')
    const saveBadge = this.page.locator('[data-state]').filter({ hasText: /^保存/ }).first()
    const recordError = this.page.locator('[role="alert"]').filter({ hasText: /加载|请求|服务/ }).first()
    return {
      saveStatus: (await saveBadge.count()) > 0 ? (await saveBadge.innerText()).trim() : null,
      recordWindowState: await grid.getAttribute('data-record-window-state').catch(() => null),
      ariaBusy: await grid.getAttribute('aria-busy').catch(() => null),
      scrollTop: await grid.evaluate((element) => element.scrollTop).catch(() => null),
      applyingViewConditions: (await this.page.getByTestId('view-conditions-applying').count()) > 0,
      loadingRecords: (await this.page.getByText('正在加载记录…', { exact: true }).count()) > 0,
      error: (await recordError.count()) > 0 ? (await recordError.innerText()).trim() : null,
      recordTitles: (await titleCells.allInnerTexts()).map((text) => text.trim()),
      groupLabels:
        (await groupButtons.count()) > 0
          ? (await groupButtons.evaluateAll((buttons) =>
              buttons.map((button) =>
                (button.getAttribute('aria-label') ?? '').replace(/^分组：/, ''),
              ),
            ))
          : [],
      groupKeys:
        (await groupButtons.count()) > 0
          ? await groupButtons.evaluateAll((buttons) =>
              buttons.map((button) =>
                button.closest('[data-group-key]')?.getAttribute('data-group-key') ?? '',
              ),
            )
          : [],
      groupFieldValue:
        (await groupSelect.count()) > 0 ? await groupSelect.inputValue().catch(() => null) : null,
    }
  }

  private classify(savedMatches: boolean): { phase: GridStallPhase; reason: string } {
    if (this.phase?.expectedConfig !== undefined && !savedMatches) {
      const save = this.latestSave()
      if (save === undefined) {
        return {
          phase: 'H47_NOT_COMPLETE',
          reason: 'No H-47 save request was observed after the scenario action began.',
        }
      }
      if (save.endedMs === undefined) {
        return {
          phase: 'H47_NOT_COMPLETE',
          reason: 'H-47 started but had not completed when the scenario failed.',
        }
      }
      return {
        phase: 'H47_NOT_COMPLETE',
        reason:
          'H-47 completed without a successful response carrying the scenario\'s current ViewConfig.',
      }
    }
    const save = this.latestSave()
    const query = this.latestQueryAfterSave(save)
    if (query === undefined) {
      return {
        phase: 'H48_NOT_STARTED',
        reason: 'The current confirmed ViewConfig has no H-48 request after its H-47 response.',
      }
    }
    if (query.endedMs === undefined) {
      return {
        phase: 'H48_STARTED_NO_RESPONSE',
        reason: 'The current H-48 request started but no response or failure had arrived.',
      }
    }
    if (
      query.status !== undefined
      && query.status >= 200
      && query.status < 300
      && responseCode(query.responseBody) === 0
    ) {
      return {
        phase: 'H48_RESPONSE_RECEIVED_UI_NOT_COMMITTED',
        reason:
          'H-48 returned a successful response, but the rendered grid did not match the expected committed data.',
      }
    }
    return {
      phase: 'UNCLASSIFIED',
      reason: 'H-48 completed unsuccessfully or the observed data cannot be classified further.',
    }
  }

  async attachOnFailure(
    testInfo: TestInfo,
    recordWriteOutcome?: unknown,
    force = false,
  ): Promise<void> {
    const failed =
      testInfo.errors.length > 0
      || testInfo.status !== testInfo.expectedStatus
    if (!force && !failed) return
    await Promise.allSettled(this.bodyTasks)

    const save = this.latestSave()
    const query = this.latestQueryAfterSave(save)
    const savedRaw = isRecord(save?.responseBody) && isRecord(save.responseBody.data)
      ? save.responseBody.data.view_config
      : undefined
    const confirmedConfig = rawViewExpectation(savedRaw)
    const expectedConfig = this.phase?.expectedConfig ?? EMPTY_CONFIG
    const savedMatches =
      save !== undefined
      && save.endedMs !== undefined
      && save.status !== undefined
      && save.status >= 200
      && save.status < 300
      && responseCode(save.responseBody) === 0
      && expectationsEqual(confirmedConfig, expectedConfig)
    const ui = await this.captureUiState().catch((failure: unknown) => ({
      captureError: failure instanceof Error ? failure.message : String(failure),
    }))
    const expectedGrid = this.phase?.expectedGrid
    const titlesMatch =
      expectedGrid !== undefined
      && JSON.stringify(ui.recordTitles) === JSON.stringify(expectedGrid.recordTitles)
    const groupsMatch =
      expectedGrid?.groupLabels === undefined
      || JSON.stringify(ui.groupLabels) === JSON.stringify(expectedGrid.groupLabels)
    const classification = this.classify(savedMatches)
    if (titlesMatch && groupsMatch) {
      classification.phase = 'UNCLASSIFIED'
      classification.reason =
        'Rendered data already matched the expected state; the failure came from another assertion phase.'
    }

    const diagnostics = {
      generatedAt: new Date().toISOString(),
      scenario: this.phase?.label ?? testInfo.title,
      phaseClassification: {
        ...classification,
        supportedPhases: [
          'H47_NOT_COMPLETE',
          'H48_NOT_STARTED',
          'H48_STARTED_NO_RESPONSE',
          'H48_RESPONSE_RECEIVED_UI_NOT_COMMITTED',
        ],
        rules: {
          H47_NOT_COMPLETE:
            'Expected H-47 is absent, unfinished, non-2xx, failed, or does not match the final ViewConfig.',
          H48_NOT_STARTED:
            'H-47 completed with the expected ViewConfig but no H-48 started afterward.',
          H48_STARTED_NO_RESPONSE:
            'The post-H-47 H-48 request started but has neither response nor failure timing.',
          H48_RESPONSE_RECEIVED_UI_NOT_COMMITTED:
            'H-48 returned HTTP 2xx plus envelope code 0 while rendered data did not match the expected window.',
        },
      },
      viewConfigSaveOutcome: {
        expected: expectedConfig,
        expectedQueryKey: recordQueryKey(this.ids.tableId, this.ids.viewId, expectedConfig),
        confirmed: confirmedConfig,
        confirmedQueryKey: recordQueryKey(this.ids.tableId, this.ids.viewId, confirmedConfig),
        request: save ?? null,
      },
      recordQueryKey: {
        inferredCurrent: recordQueryKey(this.ids.tableId, this.ids.viewId, confirmedConfig),
        expected: recordQueryKey(this.ids.tableId, this.ids.viewId, expectedConfig),
        latestRequest: query ?? null,
      },
      recordWriteOutcome: recordWriteOutcome ?? null,
      expectedVisibleData: expectedGrid ?? null,
      currentVisibleData: ui,
      networkTimeline: this.eventsAfterPhase(),
      failure: {
        message: testInfo.error?.message ?? null,
        stack: testInfo.error?.stack ?? null,
      },
    }
    const path = testInfo.outputPath('grid-stall-diagnostics.json')
    await mkdir(dirname(path), { recursive: true })
    await writeFile(path, JSON.stringify(diagnostics, null, 2), 'utf8')
    await testInfo.attach('grid-stall-diagnostics.json', {
      path,
      contentType: 'application/json',
    })
  }
}

export function groupLabelsFromOrderedValues(values: Array<string | null>): string[] {
  const labels: string[] = []
  let previous: string | null | undefined
  for (const value of values) {
    if (value !== previous) {
      labels.push(value === null || value === '' ? '空' : value)
      previous = value
    }
  }
  return labels
}

export function stringCells(rows: Array<Record<string, unknown>>, fieldId: string): Array<string | null> {
  return rows.map((row) => {
    const value = row[fieldId]
    return value === null || value === undefined || value === '' ? null : String(value)
  })
}

export function stringListCells(
  rows: Array<Record<string, unknown>>,
  fieldId: string,
  labelsByValue: ReadonlyMap<string, string>,
): Array<string | null> {
  return rows.map((row) => {
    const value = row[fieldId]
    const ids = asStringArray(value)
    if (ids.length === 0) return null
    return ids.map((id) => labelsByValue.get(id) ?? id).join('、')
  })
}

export function singleSelectCells(
  rows: Array<Record<string, unknown>>,
  fieldId: string,
  labelsByValue: ReadonlyMap<string, string>,
): Array<string | null> {
  return rows.map((row) => {
    const value = row[fieldId]
    return typeof value === 'string' && value !== '' ? labelsByValue.get(value) ?? value : null
  })
}
