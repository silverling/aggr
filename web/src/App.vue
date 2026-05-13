<script setup lang="ts">
import { ChevronsDown, Copy } from '@lucide/vue'
import { computed, nextTick, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { Toaster, toast } from 'vue-sonner'
import ApiKeyCard from './components/ApiKeyCard.vue'
import ModelAliasCard from './components/ModelAliasCard.vue'
import ModelCard from './components/ModelCard.vue'
import PendingModelDisableRulesPanel from './components/PendingModelDisableRulesPanel.vue'
import ProviderCard from './components/ProviderCard.vue'
import RequestLogCard from './components/RequestLogCard.vue'
import SectionOutline from './components/SectionOutline.vue'
import SessionCard from './components/SessionCard.vue'
import StatCard from './components/StatCard.vue'
import StatsSection from './components/StatsSection.vue'
import type {
	AuthSessionStateResponse,
	AuthSessionView,
	DeleteProxyRequestsPayload,
	ModelAliasView,
	ModelAliasesPayload,
	ModelRoute,
	CreateGatewayApiKeyResponse,
	GatewayApiKeyView,
	GatewayApiKeysPayload,
	ModelsPayload,
	NoticeTone,
	RequestStatsView,
	ProviderView,
	ProvidersPayload,
	ProxyRequestLogView,
	ProxyRequestsPayload,
	SectionOutlineItem,
	StatsRangeOption,
	SetModelDisableRulePayload,
	AuthSessionsPayload,
	PendingModelDisableRule,
} from './types'

const accessKeyForm = reactive({
	accessKey: '',
})

const apiKeyForm = reactive({
	name: '',
})

const providers = ref<ProviderView[]>([])
const models = ref<ModelRoute[]>([])
const modelAliases = ref<ModelAliasView[]>([])
const requestLogs = ref<ProxyRequestLogView[]>([])
const authState = ref<AuthSessionStateResponse | null>(null)
const sessions = ref<AuthSessionView[]>([])
const apiKeys = ref<GatewayApiKeyView[]>([])
const generatedApiKey = ref<CreateGatewayApiKeyResponse | null>(null)
const stats = ref<RequestStatsView | null>(null)
const booting = ref(true)
const loading = ref(false)
const loggingIn = ref(false)
const creatingApiKey = ref(false)
const deletingSessionId = ref<number | null>(null)
const deletingApiKeyId = ref<number | null>(null)
const statsLoading = ref(false)
const saving = ref(false)
const aliasSaving = ref(false)
const syncingAll = ref(false)
const syncingProviderId = ref<number | null>(null)
const editingProviderId = ref<number | null>(null)
const editingModelAliasId = ref<number | null>(null)
const applyingModelDisableRule = ref(false)
const clearingLogs = ref(false)
const pendingModelDisableRules = ref<PendingModelDisableRule[]>([])
const requestLogLimit = 40
const liveRefreshIntervalMs = 5000
const statsRange = ref('24h')
const statsRangeOptions: StatsRangeOption[] = [
	{ value: '1h', label: 'In an hour' },
	{ value: '24h', label: 'In 24 hours' },
	{ value: '7d', label: 'In a week' },
	{ value: '30d', label: 'In a month' },
	{ value: 'all', label: 'All' },
]

const sectionOutlineItems: SectionOutlineItem[] = [
	{ anchor: 'hero', label: 'Overview', shortLabel: '01' },
	{ anchor: 'request-stats', label: 'Stats', shortLabel: '02' },
	{ anchor: 'auth-management', label: 'Access control', shortLabel: '03' },
	{ anchor: 'provider-config', label: 'Provider config', shortLabel: '04' },
	{ anchor: 'quick-start', label: 'Quick start', shortLabel: '05' },
	{ anchor: 'model-disable-rules', label: 'Disable rules', shortLabel: '06' },
	{ anchor: 'model-aliases', label: 'Model aliases', shortLabel: '07' },
	{ anchor: 'providers', label: 'Providers', shortLabel: '08' },
	{ anchor: 'models', label: 'Models', shortLabel: '09' },
	{ anchor: 'request-logs', label: 'Request audit', shortLabel: '10' },
]

const form = reactive({
	name: '',
	baseUrl: 'https://api.openai.com/v1',
	apiKey: '',
	userAgent: '',
	enabled: true,
})

const modelAliasForm = reactive({
	aliasModelId: '',
	targetModelId: '',
	targetProviderId: '',
})

const clearRequestLogsForm = reactive({
	providerId: '',
	from: '',
	to: '',
})

const gatewayBase = computed(() => `${window.location.origin}/v1`)
const providerCount = computed(() => providers.value.length)
const enabledProviderCount = computed(() => providers.value.filter((provider) => provider.enabled).length)
const modelCount = computed(() => models.value.length)
const modelAliasCount = computed(() => modelAliases.value.length)
const duplicateCoverageCount = computed(() => models.value.filter((model) => model.providers.length > 1).length)
const requestLogCount = computed(() => requestLogs.value.length)
const requestLogList = ref<HTMLElement | null>(null)
const showRequestLogScrollCue = ref(false)
const sessionCount = computed(() => sessions.value.length)
const apiKeyCount = computed(() => apiKeys.value.length)
const statsError = ref('')
const isEditing = computed(() => editingProviderId.value !== null)
const isEditingModelAlias = computed(() => editingModelAliasId.value !== null)
const isAuthenticated = computed(() => authState.value?.authenticated ?? false)
const appVersion = computed(() => authState.value?.version ?? '')
const enabledProviderOptions = computed(() => providers.value.filter((provider) => provider.enabled))
const selectedAliasTargetProvider = computed(() => {
	if (modelAliasForm.targetProviderId === '') {
		return null
	}

	return providers.value.find((provider) => provider.id === Number(modelAliasForm.targetProviderId)) ?? null
})
const aliasTargetProviderOptions = computed(() => {
	const options = [...enabledProviderOptions.value]
	const selectedProvider = selectedAliasTargetProvider.value
	if (selectedProvider !== null && !selectedProvider.enabled && !options.some((provider) => provider.id === selectedProvider.id)) {
		options.unshift(selectedProvider)
	}

	return options
})
const targetModelOptions = computed(() => {
	const sourceProvider = selectedAliasTargetProvider.value
	const sourceProviders = sourceProvider === null ? enabledProviderOptions.value : [sourceProvider]
	const unique = new Set<string>()
	const options: string[] = []

	for (const provider of sourceProviders) {
		for (const modelId of provider.models) {
			if (unique.has(modelId)) {
				continue
			}
			unique.add(modelId)
			options.push(modelId)
		}
	}

	return options.sort((left, right) => left.localeCompare(right))
})
const activeModelDisableRules = computed(() =>
	providers.value
		.flatMap((provider) =>
			provider.disabledModels.map((modelId) => ({
				providerId: provider.id,
				providerName: provider.name,
				modelId,
			})),
		)
		.sort((left, right) => {
			if (left.providerName === right.providerName) {
				return left.modelId.localeCompare(right.modelId)
			}
			return left.providerName.localeCompare(right.providerName)
		}),
)
const pendingModelDisableRuleCount = computed(() => pendingModelDisableRules.value.length)
const featuredModel = computed(() => models.value[0]?.id ?? 'gpt-4.1')
const curlExample = computed(() =>
	[
		`curl ${gatewayBase.value}/chat/completions \\`,
		`  -H "Content-Type: application/json" \\`,
		`  -d '{`,
		`    "model": "${featuredModel.value}",`,
		`    "messages": [`,
		`      { "role": "user", "content": "Summarize the last deployment in one sentence." }`,
		`    ]`,
		`  }'`,
	].join('\n'),
)

type LoadStatsOptions = {
	background?: boolean
}

type LoadRequestLogsOptions = {
	preserveScroll?: boolean
	suppressErrors?: boolean
}

type RequestLogScrollSnapshot = {
	scrollHeight: number
	scrollTop: number
	pinnedToTop: boolean
}

function setNotice(tone: NoticeTone, text: string) {
	if (tone === 'success') {
		toast.success(text)
		return
	}
	if (tone === 'error') {
		toast.error(text)
		return
	}
	toast(text)
}

function clearNotice() {}

function findProvider(providerId: number) {
	return providers.value.find((provider) => provider.id === providerId) ?? null
}

function hasActiveModelDisableRule(providerId: number, modelId: string) {
	const provider = findProvider(providerId)
	if (provider === null) {
		return false
	}

	return provider.disabledModels.includes(modelId)
}

function pendingModelDisableRuleIndex(providerId: number, modelId: string) {
	return pendingModelDisableRules.value.findIndex((rule) => rule.providerId === providerId && rule.modelId === modelId)
}

function pendingModelDisableRuleFor(providerId: number, modelId: string) {
	return pendingModelDisableRules.value.find((rule) => rule.providerId === providerId && rule.modelId === modelId) ?? null
}

function reconcilePendingModelDisableRules() {
	pendingModelDisableRules.value = pendingModelDisableRules.value.flatMap((rule) => {
		const provider = findProvider(rule.providerId)
		if (provider === null || !provider.models.includes(rule.modelId)) {
			return []
		}

		return [
			{
				...rule,
				providerName: provider.name,
			},
		]
	})
}

function reconcileEditingModelAlias() {
	const editingId = editingModelAliasId.value
	if (editingId === null) {
		return
	}

	const alias = modelAliases.value.find((candidate) => candidate.id === editingId)
	if (alias === undefined) {
		resetModelAliasForm()
	}
}

function resetForm() {
	editingProviderId.value = null
	form.name = ''
	form.baseUrl = 'https://api.openai.com/v1'
	form.apiKey = ''
	form.userAgent = ''
	form.enabled = true
}

function resetModelAliasForm() {
	editingModelAliasId.value = null
	modelAliasForm.aliasModelId = ''
	modelAliasForm.targetModelId = ''
	modelAliasForm.targetProviderId = ''
}

function resetGeneratedApiKey() {
	generatedApiKey.value = null
	apiKeyForm.name = ''
}

function resetProtectedState() {
	statsRequestVersion += 1
	requestLogsRequestVersion += 1
	providers.value = []
	models.value = []
	modelAliases.value = []
	requestLogs.value = []
	sessions.value = []
	apiKeys.value = []
	stats.value = null
	statsError.value = ''
	statsLoading.value = false
	loading.value = false
	resetForm()
	resetModelAliasForm()
	resetRequestLogFilters()
	resetGeneratedApiKey()
	pendingModelDisableRules.value = []
}

function setLoggedOutState() {
	authState.value = {
		authenticated: false,
		version: authState.value?.version ?? '',
	}
	resetProtectedState()
}

function isStatusError(error: unknown, status: number) {
	return error instanceof Error && (error as Error & { status?: number }).status === status
}

function handleAuthError(error: unknown) {
	if (!isStatusError(error, 401)) {
		return false
	}

	setLoggedOutState()
	setNotice('error', 'Your session expired. Sign in again.')
	return true
}

async function request<T>(input: RequestInfo, init?: RequestInit): Promise<T> {
	const response = await fetch(input, init)
	const isJSON = response.headers.get('content-type')?.includes('application/json')
	const payload = isJSON ? await response.json() : null

	if (!response.ok) {
		const message = payload && typeof payload.error === 'string' ? payload.error : `${response.status} ${response.statusText}`
		const error = new Error(message) as Error & { status?: number }
		error.status = response.status
		throw error
	}

	return payload as T
}

async function loadSessionState() {
	booting.value = true
	clearNotice()

	try {
		const payload = await request<AuthSessionStateResponse>('/api/auth/session')
		authState.value = payload

		if (!payload.authenticated) {
			resetProtectedState()
			return
		}

		await loadDashboard()
	} catch (error) {
		setLoggedOutState()
		setNotice('error', error instanceof Error ? error.message : 'Failed to load the current session.')
	} finally {
		booting.value = false
	}
}

async function submitLogin() {
	loggingIn.value = true
	clearNotice()

	try {
		const payload = await request<AuthSessionStateResponse>('/api/auth/login', {
			method: 'POST',
			headers: {
				'Content-Type': 'application/json',
			},
			body: JSON.stringify({
				accessKey: accessKeyForm.accessKey,
			}),
		})

		authState.value = payload
		accessKeyForm.accessKey = ''
		await loadDashboard()
		setNotice('success', 'Signed in successfully.')
	} catch (error) {
		setNotice('error', error instanceof Error ? error.message : 'Failed to sign in.')
	} finally {
		loggingIn.value = false
	}
}

async function logout() {
	clearNotice()

	try {
		await request('/api/auth/logout', {
			method: 'POST',
		})
	} catch (error) {
		if (!isStatusError(error, 401)) {
			setNotice('error', error instanceof Error ? error.message : 'Failed to sign out.')
			return
		}
	}

	setLoggedOutState()
	setNotice('info', 'Signed out.')
}

async function loadDashboard(showNotice = false) {
	loading.value = true
	clearNotice()

	try {
		const [providerPayload, modelPayload, aliasPayload, sessionsPayload, apiKeysPayload] = await Promise.all([
			request<ProvidersPayload>('/api/providers'),
			request<ModelsPayload>('/api/models'),
			request<ModelAliasesPayload>('/api/model-aliases'),
			request<AuthSessionsPayload>('/api/auth/sessions'),
			request<GatewayApiKeysPayload>('/api/auth/api-keys'),
		])
		providers.value = providerPayload.providers
		models.value = modelPayload.models
		modelAliases.value = aliasPayload.aliases
		sessions.value = sessionsPayload.sessions
		apiKeys.value = apiKeysPayload.apiKeys
		reconcilePendingModelDisableRules()
		reconcileEditingModelAlias()
		await Promise.all([loadRequestLogs({ preserveScroll: true }), loadStats({ background: stats.value !== null })])

		if (showNotice) {
			setNotice('info', 'Dashboard refreshed.')
		}
	} catch (error) {
		if (handleAuthError(error)) {
			return
		}

		setNotice('error', error instanceof Error ? error.message : 'Failed to load dashboard.')
	} finally {
		loading.value = false
	}
}

async function submitGatewayAPIKey() {
	creatingApiKey.value = true
	clearNotice()

	try {
		const payload = await request<CreateGatewayApiKeyResponse>('/api/auth/api-keys', {
			method: 'POST',
			headers: {
				'Content-Type': 'application/json',
			},
			body: JSON.stringify({
				name: apiKeyForm.name,
			}),
		})
		generatedApiKey.value = payload
		apiKeyForm.name = ''
		await loadDashboard()
		setNotice('success', `Created API key "${payload.key.name}".`)
	} catch (error) {
		if (isStatusError(error, 401)) {
			setLoggedOutState()
			setNotice('error', 'Your session expired. Sign in again.')
			return
		}

		setNotice('error', error instanceof Error ? error.message : 'Failed to create an API key.')
	} finally {
		creatingApiKey.value = false
	}
}

async function copyGeneratedAPIKey() {
	if (generatedApiKey.value === null) {
		return
	}

	try {
		await navigator.clipboard.writeText(generatedApiKey.value.apiKey)
		setNotice('success', 'API key copied to the clipboard.')
	} catch {
		setNotice('error', 'Clipboard access is unavailable in this browser.')
	}
}

async function revokeGatewayAPIKey(apiKey: GatewayApiKeyView) {
	if (!window.confirm(`Revoke API key "${apiKey.name}"?`)) {
		return
	}

	deletingApiKeyId.value = apiKey.id
	clearNotice()

	try {
		await request(`/api/auth/api-keys/${apiKey.id}`, {
			method: 'DELETE',
		})
		if (generatedApiKey.value?.key.id === apiKey.id) {
			resetGeneratedApiKey()
		}
		await loadDashboard()
		setNotice('success', `Revoked API key "${apiKey.name}".`)
	} catch (error) {
		if (isStatusError(error, 401)) {
			setLoggedOutState()
			setNotice('error', 'Your session expired. Sign in again.')
			return
		}

		setNotice('error', error instanceof Error ? error.message : `Failed to revoke API key "${apiKey.name}".`)
	} finally {
		deletingApiKeyId.value = null
	}
}

async function revokeSession(session: AuthSessionView) {
	if (!window.confirm(`Revoke session #${session.id}?`)) {
		return
	}

	deletingSessionId.value = session.id
	clearNotice()

	try {
		await request(`/api/auth/sessions/${session.id}`, {
			method: 'DELETE',
		})

		if (session.current) {
			setLoggedOutState()
			setNotice('info', 'The current session was revoked. Sign in again to continue.')
			return
		}

		await loadDashboard()
		setNotice('success', `Revoked session #${session.id}.`)
	} catch (error) {
		if (isStatusError(error, 401)) {
			setLoggedOutState()
			setNotice('error', 'Your session expired. Sign in again.')
			return
		}

		setNotice('error', error instanceof Error ? error.message : `Failed to revoke session #${session.id}.`)
	} finally {
		deletingSessionId.value = null
	}
}

let statsRequestVersion = 0
let requestLogsRequestVersion = 0
let liveRefreshTimer: number | null = null
let liveRefreshInFlight = false

async function loadStats(options: LoadStatsOptions = {}) {
	if (!isAuthenticated.value) {
		stats.value = null
		statsError.value = ''
		statsLoading.value = false
		return
	}

	const requestVersion = ++statsRequestVersion
	const showLoading = !options.background || stats.value === null
	if (showLoading) {
		statsLoading.value = true
		statsError.value = ''
	}

	try {
		const params = new URLSearchParams({
			range: statsRange.value,
			timeZone: Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC',
		})
		const payload = await request<RequestStatsView>(`/api/stats?${params.toString()}`)
		if (requestVersion !== statsRequestVersion) {
			return
		}
		stats.value = payload
		statsError.value = ''
	} catch (error) {
		if (requestVersion !== statsRequestVersion) {
			return
		}
		if (isStatusError(error, 401)) {
			setLoggedOutState()
			setNotice('error', 'Your session expired. Sign in again.')
			return
		}
		statsError.value = error instanceof Error ? error.message : 'Failed to load stats.'
	} finally {
		if (showLoading && requestVersion === statsRequestVersion) {
			statsLoading.value = false
		}
	}
}

function captureRequestLogScrollSnapshot(): RequestLogScrollSnapshot | null {
	const element = requestLogList.value
	if (element === null) {
		return null
	}

	return {
		scrollHeight: element.scrollHeight,
		scrollTop: element.scrollTop,
		pinnedToTop: element.scrollTop <= 16,
	}
}

function restoreRequestLogScrollSnapshot(snapshot: RequestLogScrollSnapshot | null) {
	if (snapshot === null) {
		return
	}

	void nextTick(() => {
		const element = requestLogList.value
		if (element === null || snapshot.pinnedToTop) {
			return
		}

		const heightDelta = element.scrollHeight - snapshot.scrollHeight
		element.scrollTop = Math.max(0, snapshot.scrollTop + heightDelta)
		syncRequestLogScrollCue()
	})
}

async function loadRequestLogs(options: LoadRequestLogsOptions = {}) {
	if (!isAuthenticated.value) {
		requestLogs.value = []
		return
	}

	const requestVersion = ++requestLogsRequestVersion
	const scrollSnapshot = options.preserveScroll ? captureRequestLogScrollSnapshot() : null

	try {
		const payload = await request<ProxyRequestsPayload>(`/api/requests?limit=${requestLogLimit}`)
		if (requestVersion !== requestLogsRequestVersion) {
			return
		}

		requestLogs.value = payload.requests
		restoreRequestLogScrollSnapshot(scrollSnapshot)
	} catch (error) {
		if (requestVersion !== requestLogsRequestVersion) {
			return
		}
		if (handleAuthError(error) || options.suppressErrors) {
			return
		}

		throw error
	}
}

async function refreshLivePanels() {
	if (liveRefreshInFlight || booting.value || loading.value || statsLoading.value || !isAuthenticated.value) {
		return
	}
	if (document.visibilityState !== 'visible') {
		return
	}

	liveRefreshInFlight = true

	try {
		await Promise.all([loadStats({ background: true }), loadRequestLogs({ preserveScroll: true, suppressErrors: true })])
	} finally {
		liveRefreshInFlight = false
	}
}

function handleVisibilityChange() {
	if (document.visibilityState !== 'visible') {
		return
	}

	void refreshLivePanels()
}

function beginEdit(provider: ProviderView) {
	editingProviderId.value = provider.id
	form.name = provider.name
	form.baseUrl = provider.baseUrl
	form.apiKey = ''
	form.userAgent = provider.userAgent ?? ''
	form.enabled = provider.enabled
	clearNotice()
	window.scrollTo({ top: 0, behavior: 'smooth' })
}

function beginEditModelAlias(alias: ModelAliasView) {
	editingModelAliasId.value = alias.id
	modelAliasForm.aliasModelId = alias.aliasModelId
	modelAliasForm.targetModelId = alias.targetModelId
	modelAliasForm.targetProviderId = alias.targetProviderId === undefined ? '' : String(alias.targetProviderId)
	clearNotice()
	window.scrollTo({ top: 0, behavior: 'smooth' })
}

async function submitProvider() {
	saving.value = true
	clearNotice()

	const method = editingProviderId.value === null ? 'POST' : 'PUT'
	const endpoint = editingProviderId.value === null ? '/api/providers' : `/api/providers/${editingProviderId.value}`

	try {
		await request(endpoint, {
			method,
			headers: {
				'Content-Type': 'application/json',
			},
			body: JSON.stringify({
				name: form.name,
				baseUrl: form.baseUrl,
				apiKey: form.apiKey,
				userAgent: form.userAgent,
				enabled: form.enabled,
			}),
		})

		resetForm()
		await loadDashboard()
		setNotice('success', method === 'POST' ? 'Provider created and synced.' : 'Provider updated and synced.')
	} catch (error) {
		if (handleAuthError(error)) {
			return
		}
		setNotice('error', error instanceof Error ? error.message : 'Failed to save provider.')
	} finally {
		saving.value = false
	}
}

async function syncProvider(provider: ProviderView) {
	syncingProviderId.value = provider.id
	clearNotice()

	try {
		await request(`/api/providers/${provider.id}/sync`, {
			method: 'POST',
		})
		await loadDashboard()
		setNotice('success', `Synced models for ${provider.name}.`)
	} catch (error) {
		if (handleAuthError(error)) {
			return
		}
		setNotice('error', error instanceof Error ? error.message : `Failed to sync ${provider.name}.`)
	} finally {
		syncingProviderId.value = null
	}
}

async function syncAll() {
	syncingAll.value = true
	clearNotice()

	try {
		await request('/api/providers/sync', {
			method: 'POST',
		})
		await loadDashboard()
		setNotice('success', 'Synced every provider catalog.')
	} catch (error) {
		if (handleAuthError(error)) {
			return
		}
		setNotice('error', error instanceof Error ? error.message : 'Failed to sync providers.')
	} finally {
		syncingAll.value = false
	}
}

function toggleModelDisableRule(provider: Pick<ProviderView, 'id' | 'name'>, modelId: string) {
	const existingIndex = pendingModelDisableRuleIndex(provider.id, modelId)
	if (existingIndex >= 0) {
		pendingModelDisableRules.value.splice(existingIndex, 1)
		clearNotice()
		return
	}

	pendingModelDisableRules.value = [
		...pendingModelDisableRules.value,
		{
			providerId: provider.id,
			providerName: provider.name,
			modelId,
			disabled: !hasActiveModelDisableRule(provider.id, modelId),
		},
	]
	clearNotice()
}

async function removeProvider(provider: ProviderView) {
	if (!window.confirm(`Delete provider "${provider.name}"?`)) {
		return
	}

	clearNotice()

	try {
		await request(`/api/providers/${provider.id}`, {
			method: 'DELETE',
		})

		if (editingProviderId.value === provider.id) {
			resetForm()
		}

		await loadDashboard()
		setNotice('success', `Deleted ${provider.name}.`)
	} catch (error) {
		if (handleAuthError(error)) {
			return
		}
		setNotice('error', error instanceof Error ? error.message : `Failed to delete ${provider.name}.`)
	}
}

async function submitModelAlias() {
	aliasSaving.value = true
	clearNotice()

	const method = editingModelAliasId.value === null ? 'POST' : 'PUT'
	const endpoint = editingModelAliasId.value === null ? '/api/model-aliases' : `/api/model-aliases/${editingModelAliasId.value}`

	try {
		await request(endpoint, {
			method,
			headers: {
				'Content-Type': 'application/json',
			},
			body: JSON.stringify({
				aliasModelId: modelAliasForm.aliasModelId,
				targetModelId: modelAliasForm.targetModelId,
				targetProviderId: modelAliasForm.targetProviderId === '' ? undefined : Number(modelAliasForm.targetProviderId),
			}),
		})

		resetModelAliasForm()
		await loadDashboard()
		setNotice('success', method === 'POST' ? 'Model alias created.' : 'Model alias updated.')
	} catch (error) {
		if (handleAuthError(error)) {
			return
		}
		setNotice('error', error instanceof Error ? error.message : 'Failed to save model alias.')
	} finally {
		aliasSaving.value = false
	}
}

async function removeModelAlias(alias: ModelAliasView) {
	if (!window.confirm(`Delete model alias "${alias.aliasModelId}"?`)) {
		return
	}

	clearNotice()

	try {
		await request(`/api/model-aliases/${alias.id}`, {
			method: 'DELETE',
		})

		if (editingModelAliasId.value === alias.id) {
			resetModelAliasForm()
		}

		await loadDashboard()
		setNotice('success', `Deleted ${alias.aliasModelId}.`)
	} catch (error) {
		if (handleAuthError(error)) {
			return
		}
		setNotice('error', error instanceof Error ? error.message : `Failed to delete ${alias.aliasModelId}.`)
	}
}

function removePendingModelDisableRule(rule: PendingModelDisableRule) {
	const existingIndex = pendingModelDisableRuleIndex(rule.providerId, rule.modelId)
	if (existingIndex < 0) {
		return
	}

	pendingModelDisableRules.value.splice(existingIndex, 1)
}

function clearPendingModelDisableRules() {
	pendingModelDisableRules.value = []
}

function updateStatsRange(value: string) {
	statsRange.value = value
}

function resetRequestLogFilters() {
	clearRequestLogsForm.providerId = ''
	clearRequestLogsForm.from = ''
	clearRequestLogsForm.to = ''
}

function toRFC3339(value: string, label: string) {
	if (!value) {
		return ''
	}

	const parsed = new Date(value)
	if (Number.isNaN(parsed.valueOf())) {
		throw new Error(`Invalid ${label}.`)
	}

	return parsed.toISOString()
}

async function copyGatewayBase() {
	try {
		await navigator.clipboard.writeText(gatewayBase.value)
		setNotice('success', 'Gateway base copied to the clipboard.')
	} catch {
		setNotice('error', 'Clipboard access is unavailable in this browser.')
	}
}

async function applyModelDisableRule() {
	if (pendingModelDisableRules.value.length === 0) {
		return
	}

	applyingModelDisableRule.value = true
	clearNotice()
	const pendingRules = [...pendingModelDisableRules.value]
	const payload: SetModelDisableRulePayload = {
		rules: pendingRules.map((rule) => ({
			providerId: rule.providerId,
			modelId: rule.modelId,
			disabled: rule.disabled,
		})),
	}

	try {
		await request<SetModelDisableRulePayload>('/api/model-disable-rules', {
			method: 'PUT',
			headers: {
				'Content-Type': 'application/json',
			},
			body: JSON.stringify(payload),
		})
		const disabledCount = pendingRules.filter((rule) => rule.disabled).length
		const enabledCount = pendingRules.length - disabledCount
		await loadDashboard()
		clearPendingModelDisableRules()
		setNotice(
			'success',
			[
				disabledCount > 0 ? `Disabled ${disabledCount} route${disabledCount === 1 ? '' : 's'}` : '',
				enabledCount > 0 ? `re-enabled ${enabledCount} route${enabledCount === 1 ? '' : 's'}` : '',
			]
				.filter((part) => part !== '')
				.join(' and ') + '.',
		)
	} catch (error) {
		if (handleAuthError(error)) {
			return
		}
		setNotice('error', error instanceof Error ? error.message : 'Failed to update the model disable rule.')
	} finally {
		applyingModelDisableRule.value = false
	}
}

async function clearLogs() {
	const hasFilters = clearRequestLogsForm.providerId !== '' || clearRequestLogsForm.from !== '' || clearRequestLogsForm.to !== ''
	if (!window.confirm(hasFilters ? 'Clear the request logs that match the selected filters?' : 'Clear every recorded request log?')) {
		return
	}

	clearingLogs.value = true
	clearNotice()

	try {
		const params = new URLSearchParams()
		if (clearRequestLogsForm.providerId !== '') {
			params.set('providerId', clearRequestLogsForm.providerId)
		}

		const from = toRFC3339(clearRequestLogsForm.from, 'start date')
		const to = toRFC3339(clearRequestLogsForm.to, 'end date')
		if (from) {
			params.set('from', from)
		}
		if (to) {
			params.set('to', to)
		}

		const suffix = params.toString() === '' ? '' : `?${params.toString()}`
		const payload = await request<DeleteProxyRequestsPayload>(`/api/requests${suffix}`, {
			method: 'DELETE',
		})
		await loadDashboard()

		if (payload.deleted === 0) {
			setNotice('info', 'No request logs matched the selected filters.')
			return
		}

		setNotice('success', `Deleted ${payload.deleted} request log${payload.deleted === 1 ? '' : 's'}.`)
	} catch (error) {
		if (handleAuthError(error)) {
			return
		}
		setNotice('error', error instanceof Error ? error.message : 'Failed to clear request logs.')
	} finally {
		clearingLogs.value = false
	}
}

function syncRequestLogScrollCue() {
	const element = requestLogList.value
	if (element === null) {
		showRequestLogScrollCue.value = false
		return
	}

	const remainingScroll = element.scrollHeight - element.clientHeight - element.scrollTop
	showRequestLogScrollCue.value = remainingScroll > 12
}

function syncRequestLogScrollCueAfterRender() {
	void nextTick(() => {
		syncRequestLogScrollCue()
	})
}

watch(statsRange, () => {
	void loadStats()
})

watch(requestLogs, () => {
	syncRequestLogScrollCueAfterRender()
})

onMounted(() => {
	window.addEventListener('resize', syncRequestLogScrollCue)
	document.addEventListener('visibilitychange', handleVisibilityChange)
	liveRefreshTimer = window.setInterval(() => {
		void refreshLivePanels()
	}, liveRefreshIntervalMs)
	void loadSessionState()
})

onBeforeUnmount(() => {
	window.removeEventListener('resize', syncRequestLogScrollCue)
	document.removeEventListener('visibilitychange', handleVisibilityChange)
	if (liveRefreshTimer !== null) {
		window.clearInterval(liveRefreshTimer)
		liveRefreshTimer = null
	}
})
</script>

<template>
	<Toaster richColors position="top-right" />
	<div data-anchor="dashboard" class="mx-auto grid w-[min(1240px,calc(100vw-32px))] gap-5.5 py-8 max-lg:w-[calc(100vw-24px)] max-lg:py-4">
		<!-- Loading Screen -->
		<div
			v-if="booting"
			class="grid gap-6 overflow-hidden rounded-panel border border-line bg-surface p-5 shadow-panel backdrop-blur-[18px] lg:p-8.5"
		>
			<div class="grid gap-3">
				<p class="text-xs font-bold uppercase tracking-widest text-accent">Loading</p>
				<h1>Aggr</h1>
				<p class="max-w-[58ch] text-[1.04rem] leading-[1.65] text-ink-soft">Checking your login session and loading the dashboard…</p>
			</div>
		</div>

		<!-- Login Panel -->
		<div
			v-else-if="!isAuthenticated"
			data-anchor="auth-login"
			class="grid gap-7 overflow-hidden rounded-panel border border-line bg-surface p-5 shadow-panel backdrop-blur-[18px] lg:grid-cols-[minmax(0,1.1fr)_minmax(0,0.9fr)] lg:p-8.5"
		>
			<div class="grid gap-4">
				<div class="max-w-190">
					<p class="mb-3 text-xs font-bold uppercase tracking-widest text-accent">Access control</p>
					<h1>Aggr</h1>
					<p class="mt-4 max-w-[58ch] text-[1.04rem] leading-[1.65] text-ink-soft">
						Sign in with the shared access key to manage providers, inspect traffic, and issue gateway API keys.
					</p>
					<p v-if="appVersion" class="mt-3 text-xs font-bold uppercase tracking-[0.14em] text-ink-soft">Version {{ appVersion }}</p>
				</div>

				<div class="rounded-card border border-line bg-surface-strong p-4.5">
					<p class="text-sm font-bold uppercase tracking-[0.14em] text-accent-strong">How it works</p>
					<ul class="mt-4 grid gap-2.5 pl-4 leading-[1.55] text-ink-soft">
						<li>
							The access key comes from <code class="font-mono text-ink-strong">AGGR_ACCESS_KEY</code> in the
							<code class="font-mono text-ink-strong">.env</code> file.
						</li>
						<li>Browser sessions are stored in SQLite and shown below after login.</li>
						<li>Bare `/v1` requests need a gateway API key created in this dashboard.</li>
					</ul>
				</div>
			</div>

			<form class="grid gap-4 rounded-card border border-line bg-surface-strong p-4.5" @submit.prevent="submitLogin">
				<div>
					<p class="mb-3 text-xs font-bold uppercase tracking-widest text-accent">Login</p>
					<h2>Enter the shared access key</h2>
				</div>
				<label class="grid gap-2">
					<span class="text-[0.92rem] font-bold text-ink-strong">Access key</span>
					<input
						v-model.trim="accessKeyForm.accessKey"
						class="w-full rounded-(--radius-field) border border-line-strong bg-white/90 px-4 py-3.75 text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
						type="password"
						autocomplete="current-password"
						placeholder="Enter the shared access key"
						required
					/>
				</label>
				<button
					class="inline-flex min-h-12 items-center justify-center rounded-full border border-transparent bg-[linear-gradient(135deg,var(--color-accent),#0f9275)] px-4.5 font-bold text-[#f7fffc] transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60"
					type="submit"
					:disabled="loggingIn"
				>
					{{ loggingIn ? 'Signing in…' : 'Sign in' }}
				</button>
			</form>
		</div>

		<!-- Dashboard -->
		<template v-else>
			<div class="relative">
				<SectionOutline :items="sectionOutlineItems" />
				<PendingModelDisableRulesPanel
					:rules="pendingModelDisableRules"
					:applying="applyingModelDisableRule"
					@remove-rule="removePendingModelDisableRule"
					@clear="clearPendingModelDisableRules"
					@apply="applyModelDisableRule"
				/>

				<div class="grid min-w-0 gap-5.5">
					<header
						data-anchor="hero"
						class="grid gap-7 overflow-hidden rounded-panel border border-line bg-surface p-5 shadow-panel backdrop-blur-[18px] lg:p-8.5"
					>
						<div class="max-w-190">
							<p class="mb-3 flex items-center gap-2">
								<span class="text-xs font-bold uppercase tracking-widest text-accent">Unified gateway</span>
								<span
									v-if="appVersion"
									class="inline-flex items-center rounded-full border border-line bg-surface-strong px-3 py-1 font-mono text-[0.78rem] font-bold text-ink-strong"
								>
									{{ appVersion }}
								</span>
							</p>
							<h1>Aggr</h1>
						</div>

						<div class="flex flex-wrap items-center gap-3 max-lg:flex-col max-lg:items-stretch">
							<button
								class="inline-flex min-h-12 items-center justify-center rounded-full border border-line bg-[rgba(255,255,255,0.72)] px-4.5 font-bold text-ink-strong transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
								type="button"
								:disabled="loading"
								@click="loadDashboard(true)"
							>
								{{ loading ? 'Refreshing…' : 'Refresh dashboard' }}
							</button>
							<button
								class="inline-flex min-h-12 items-center justify-center rounded-full border border-transparent bg-[linear-gradient(135deg,var(--color-accent),#0f9275)] px-4.5 font-bold text-[#f7fffc] transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
								type="button"
								:disabled="syncingAll"
								@click="syncAll"
							>
								{{ syncingAll ? 'Syncing catalogs…' : 'Sync all providers' }}
							</button>
							<button
								class="ml-auto inline-flex min-h-12 items-center justify-center rounded-full border border-line bg-[rgba(255,255,255,0.72)] px-4.5 font-bold text-ink-strong transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
								type="button"
								@click="logout"
							>
								Sign out
							</button>
						</div>

						<div class="grid gap-4.5 md:grid-cols-2 xl:grid-cols-4">
							<StatCard label="Providers" :value="providerCount" :description="`${enabledProviderCount} enabled for routing`" />
							<StatCard label="Models" :value="modelCount" description="From synced `/v1/models` catalogs" />
							<StatCard label="Aliases" :value="modelAliasCount" description="Public model names mapped to upstream targets" />
							<StatCard label="Coverage overlap" :value="duplicateCoverageCount" description="Models offered by multiple providers" />
						</div>
					</header>

					<StatsSection
						:stats="stats"
						:range="statsRange"
						:range-options="statsRangeOptions"
						:loading="statsLoading"
						:error="statsError"
						@update:range="updateStatsRange"
					/>

					<section
						data-anchor="auth-management"
						class="rounded-panel border border-line bg-surface p-5 shadow-panel backdrop-blur-[18px] lg:p-7"
					>
						<div class="mb-5 flex items-start justify-between gap-3 max-lg:flex-col max-lg:items-stretch">
							<div>
								<p class="mb-3 text-xs font-bold uppercase tracking-widest text-accent">Access control</p>
								<h2>Browser sessions and API keys</h2>
								<p class="mt-1.5 leading-[1.6] text-ink-soft">
									Browser sessions unlock the admin UI; gateway API keys are required for `/v1` requests.
								</p>
							</div>
							<span class="text-ink-soft">{{ sessionCount }} sessions / {{ apiKeyCount }} API keys</span>
						</div>

						<div class="grid gap-4.5 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
							<article data-anchor="api-key-manager" class="grid gap-4 rounded-card border border-line bg-surface-strong p-4.5">
								<div>
									<h3>Create gateway API keys</h3>
									<p class="mt-1.5 leading-[1.6] text-ink-soft">
										Use these bearer tokens when calling the gateway&apos;s OpenAI-like `/v1` endpoints.
									</p>
								</div>

								<form class="grid gap-4" @submit.prevent="submitGatewayAPIKey">
									<label class="grid gap-2">
										<span class="text-[0.92rem] font-bold text-ink-strong">Key name</span>
										<input
											v-model.trim="apiKeyForm.name"
											class="w-full rounded-(--radius-field) border border-line-strong bg-white/90 px-4 py-3.75 text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
											type="text"
											autocomplete="off"
											placeholder="Some Client"
											required
										/>
									</label>

									<button
										class="inline-flex min-h-12 items-center justify-center rounded-full border border-transparent bg-[linear-gradient(135deg,var(--color-accent),#0f9275)] px-4.5 font-bold text-[#f7fffc] transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60"
										type="submit"
										:disabled="creatingApiKey"
									>
										{{ creatingApiKey ? 'Creating…' : 'Create API key' }}
									</button>
								</form>

								<div
									v-if="generatedApiKey"
									data-anchor="api-key-reveal"
									class="grid gap-3 rounded-[18px] border border-accent-soft bg-accent-soft p-4"
								>
									<div>
										<p class="text-xs font-bold uppercase tracking-widest text-accent">Shown once</p>
										<h3 class="mt-1">Copy this key now</h3>
									</div>
									<code class="wrap-break-word rounded-[16px] border border-line bg-white/75 px-3.5 py-3 text-[0.84rem] text-ink">{{
										generatedApiKey.apiKey
									}}</code>
									<div class="flex flex-wrap items-center justify-start gap-3 max-lg:flex-col max-lg:items-stretch">
										<button
											class="inline-flex min-h-12 items-center justify-center rounded-full border border-line bg-[rgba(255,255,255,0.72)] px-4.5 font-bold text-ink-strong transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
											type="button"
											@click="copyGeneratedAPIKey"
										>
											Copy key
										</button>
										<button
											class="inline-flex min-h-12 items-center justify-center rounded-full border border-line bg-[rgba(255,255,255,0.72)] px-4.5 font-bold text-ink-strong transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
											type="button"
											@click="resetGeneratedApiKey"
										>
											Dismiss
										</button>
									</div>
								</div>

								<p
									v-if="apiKeys.length === 0"
									class="rounded-[16px] border border-dashed border-line bg-white/50 px-3.5 py-4 leading-[1.6] text-ink-soft"
								>
									No gateway API keys have been created yet.
								</p>

								<div v-else class="grid gap-3">
									<ApiKeyCard
										v-for="apiKey in apiKeys"
										:key="apiKey.id"
										:api-key="apiKey"
										:deleting="deletingApiKeyId === apiKey.id"
										@delete="revokeGatewayAPIKey(apiKey)"
									/>
								</div>
							</article>

							<article data-anchor="session-manager" class="grid gap-4 rounded-card border border-line bg-surface-strong p-4.5">
								<div>
									<h3>Logged in sessions</h3>
									<p class="mt-1.5 leading-[1.6] text-ink-soft">
										Each login creates a database-backed cookie session. Revoke one here to force the browser to sign in again.
									</p>
								</div>

								<p
									v-if="sessions.length === 0"
									class="rounded-[16px] border border-dashed border-line bg-white/50 px-3.5 py-4 leading-[1.6] text-ink-soft"
								>
									No browser sessions are active yet.
								</p>

								<div v-else class="grid gap-3">
									<SessionCard
										v-for="session in sessions"
										:key="session.id"
										:session="session"
										:deleting="deletingSessionId === session.id"
										@delete="revokeSession(session)"
									/>
								</div>
							</article>
						</div>
					</section>

					<section class="grid gap-4.5 lg:grid-cols-[minmax(0,1.1fr)_minmax(0,0.9fr)]">
						<article
							data-anchor="provider-config"
							class="rounded-panel border border-line bg-surface p-5 shadow-panel backdrop-blur-[18px] lg:p-7"
						>
							<div class="mb-5 flex items-start justify-between gap-3">
								<div>
									<p class="mb-3 text-xs font-bold uppercase tracking-widest text-accent">Provider config</p>
									<h2>{{ isEditing ? 'Update an upstream provider' : 'Add an upstream provider' }}</h2>
								</div>
								<button v-if="isEditing" class="border-0 bg-transparent p-0 font-bold text-accent" type="button" @click="resetForm">
									Cancel edit
								</button>
							</div>

							<form class="grid gap-4" @submit.prevent="submitProvider">
								<label class="grid gap-2">
									<span class="text-[0.92rem] font-bold text-ink-strong">Display name</span>
									<input
										v-model.trim="form.name"
										class="w-full rounded-(--radius-field) border border-line-strong bg-white/90 px-4 py-3.75 text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
										type="text"
										autocomplete="off"
										placeholder="OpenAI primary"
										required
									/>
								</label>

								<label class="grid gap-2">
									<span class="text-[0.92rem] font-bold text-ink-strong">Base URL</span>
									<input
										v-model.trim="form.baseUrl"
										class="w-full rounded-(--radius-field) border border-line-strong bg-white/90 px-4 py-3.75 text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
										type="url"
										autocomplete="off"
										placeholder="https://api.openai.com/v1"
										required
									/>
									<small class="text-ink-soft">Use the provider&apos;s OpenAI-compatible API root.</small>
								</label>

								<label class="grid gap-2">
									<span class="text-[0.92rem] font-bold text-ink-strong">API key</span>
									<input
										v-model.trim="form.apiKey"
										class="w-full rounded-(--radius-field) border border-line-strong bg-white/90 px-4 py-3.75 text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
										type="password"
										:placeholder="isEditing ? 'Leave blank to keep the current key' : 'sk-...'"
										:required="!isEditing"
									/>
								</label>

								<label class="grid gap-2">
									<span class="text-[0.92rem] font-bold text-ink-strong">User agent</span>
									<input
										v-model.trim="form.userAgent"
										class="w-full rounded-(--radius-field) border border-line-strong bg-white/90 px-4 py-3.75 text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
										type="text"
										autocomplete="off"
										placeholder="Aggr/1.0"
									/>
									<small class="text-ink-soft">Leave blank to use the SDK default upstream user agent.</small>
								</label>

								<label class="flex items-center justify-start gap-3 rounded-(--radius-field) border border-line bg-surface-muted px-4 py-3.5">
									<input v-model="form.enabled" class="h-4.5 w-4.5 accent-accent" type="checkbox" />
									<span class="font-bold text-ink-strong">Enabled for model routing</span>
								</label>

								<button
									class="inline-flex min-h-12 items-center justify-center rounded-full border border-transparent bg-[linear-gradient(135deg,var(--color-accent),#0f9275)] px-4.5 font-bold text-[#f7fffc] transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60"
									type="submit"
									:disabled="saving"
								>
									{{ saving ? 'Saving…' : isEditing ? 'Update provider' : 'Create provider' }}
								</button>
							</form>
						</article>

						<article data-anchor="quick-start" class="rounded-panel border border-line bg-surface p-5 shadow-panel backdrop-blur-[18px] lg:p-7">
							<div class="mb-5 flex items-start justify-between gap-3">
								<div>
									<p class="mb-3 text-xs font-bold uppercase tracking-widest text-accent">Quick start</p>
									<h2>Point clients at the gateway</h2>
								</div>
							</div>

							<div class="mb-4 flex flex-wrap items-center justify-between gap-3 rounded-card border border-line bg-surface-strong p-4.5">
								<span class="text-[0.82rem] font-bold uppercase tracking-[0.18em] text-accent-strong">Gateway base</span>
								<code class="wrap-break-word text-ink-strong">{{ gatewayBase }}</code>
								<button class="border-0 bg-transparent p-0 font-bold text-accent" type="button" @click="copyGatewayBase">
									<Copy class="size-5" />
								</button>
							</div>

							<pre
								class="m-0 overflow-auto rounded-card border border-line bg-[linear-gradient(180deg,rgba(14,32,41,0.96),rgba(14,32,41,0.88)),radial-gradient(circle_at_top_left,rgba(12,118,98,0.28),transparent_55%)] p-4.5 text-[#dff7f1]"
							><code class="whitespace-pre-wrap wrap-break-word text-[0.92rem] leading-[1.75]">{{ curlExample }}</code></pre>

							<ul class="mt-4 grid gap-2.5 pl-4 leading-[1.55] text-ink-soft">
								<li><code class="font-mono text-ink-strong">GET /v1/models</code> returns the aggregated model catalog.</li>
								<li>Requests are routed strictly by the <code class="font-mono text-ink-strong">model</code> field in the JSON payload.</li>
								<li>Providers sync automatically after create or update, and you can resync at any time.</li>
							</ul>
						</article>
					</section>

					<section
						data-anchor="model-disable-rules"
						class="rounded-panel border border-line bg-surface p-5 shadow-panel backdrop-blur-[18px] lg:p-7"
					>
						<div class="mb-5 flex items-start justify-between gap-3 max-lg:flex-col max-lg:items-stretch">
							<div>
								<p class="mb-3 text-xs font-bold uppercase tracking-widest text-accent">Model disable rules</p>
								<h2>Stage provider/model route changes</h2>
							</div>
							<span class="text-ink-soft">{{ pendingModelDisableRuleCount }} staged / {{ activeModelDisableRules.length }} active</span>
						</div>

						<div class="grid gap-4.5 lg:grid-cols-[minmax(0,0.82fr)_minmax(0,1.18fr)]">
							<article data-anchor="model-disable-rule-guide" class="grid gap-4 rounded-card border border-line bg-surface-strong p-4.5">
								<div>
									<h3>How to use it</h3>
									<p class="mt-1.5 leading-[1.6] text-ink-soft">
										Click a provider chip in a model card or a model chip in a provider card to stage the inverse of its current route state.
										Apply staged changes together from the floating pending card.
									</p>
								</div>

								<ul class="grid gap-2.5 pl-4 leading-[1.55] text-ink-soft">
									<li>Red queued chips will create disable rules.</li>
									<li>Green queued chips will remove existing disable rules.</li>
									<li>Click the same chip again to unstage it before applying.</li>
								</ul>

								<div
									v-if="pendingModelDisableRuleCount === 0"
									class="rounded-[16px] border border-dashed border-line bg-white/50 px-3.5 py-4 leading-[1.6] text-ink-soft min-[1848px]:hidden"
								>
									No pending route changes are staged yet.
								</div>

								<div v-else class="grid gap-3 min-[1848px]:hidden">
									<div
										v-for="rule in pendingModelDisableRules"
										:key="`${rule.providerId}:${rule.modelId}`"
										class="rounded-[18px] border border-line bg-white/80 p-3"
									>
										<div class="flex items-start justify-between gap-3">
											<div class="min-w-0">
												<p :class="['text-[0.72rem] font-bold uppercase tracking-[0.16em]', rule.disabled ? 'text-danger' : 'text-accent']">
													{{ rule.disabled ? 'Disable' : 'Re-enable' }}
												</p>
												<p class="mt-1 wrap-break-word font-mono text-[0.82rem] font-bold text-ink-strong">
													{{ rule.providerName }} · {{ rule.modelId }}
												</p>
											</div>
											<button
												class="border-0 bg-transparent p-0 text-sm font-bold text-ink-soft transition duration-150 ease-out hover:text-ink-strong"
												type="button"
												@click="removePendingModelDisableRule(rule)"
											>
												Remove
											</button>
										</div>
									</div>

									<div class="grid gap-2.5">
										<button
											class="inline-flex min-h-12 items-center justify-center rounded-full border border-transparent bg-[linear-gradient(135deg,var(--color-accent),#0f9275)] px-4.5 font-bold text-[#f7fffc] transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60"
											type="button"
											:disabled="applyingModelDisableRule"
											@click="applyModelDisableRule"
										>
											{{
												applyingModelDisableRule
													? 'Applying…'
													: `Apply ${pendingModelDisableRuleCount} change${pendingModelDisableRuleCount === 1 ? '' : 's'}`
											}}
										</button>
										<button
											class="inline-flex min-h-12 items-center justify-center rounded-full border border-line bg-[rgba(255,255,255,0.72)] px-4.5 font-bold text-ink-strong transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60"
											type="button"
											:disabled="applyingModelDisableRule"
											@click="clearPendingModelDisableRules"
										>
											Clear all
										</button>
									</div>
								</div>
							</article>

							<article
								data-anchor="model-disable-rule-active"
								class="flex flex-col gap-4 rounded-card border border-line bg-surface-strong p-4.5"
							>
								<div>
									<h3>Current rules</h3>
									<p class="mt-1.5 leading-[1.6] text-ink-soft">
										Disabled routes stay out of `/v1/models` and proxy selection until you remove the rule.
									</p>
								</div>

								<p
									v-if="activeModelDisableRules.length === 0"
									class="rounded-[16px] border border-dashed border-line bg-white/50 px-3.5 py-4 leading-[1.6] text-ink-soft"
								>
									No provider/model pairs are disabled right now.
								</p>

								<div v-else class="flex flex-wrap gap-2.5">
									<button
										v-for="rule in activeModelDisableRules"
										:key="`${rule.providerId}:${rule.modelId}`"
										data-anchor="model-disable-rule-chip"
										:class="[
											'inline-flex items-center rounded-full border px-3 py-2 font-mono text-[0.82rem] font-bold transition duration-150 ease-out hover:-translate-y-px',
											pendingModelDisableRuleFor(rule.providerId, rule.modelId) !== null
												? 'border-[rgba(12,118,98,0.24)] bg-[rgba(12,118,98,0.12)] text-accent shadow-[0_10px_24px_rgba(24,34,47,0.08)]'
												: 'border-[rgba(164,63,63,0.18)] bg-danger-soft text-danger',
										]"
										type="button"
										@click="toggleModelDisableRule({ id: rule.providerId, name: rule.providerName }, rule.modelId)"
									>
										{{ rule.providerName }} · {{ rule.modelId }}
									</button>
								</div>
							</article>
						</div>
					</section>

					<section data-anchor="model-aliases" class="rounded-panel border border-line bg-surface p-5 shadow-panel backdrop-blur-[18px] lg:p-7">
						<div class="mb-5 flex items-start justify-between gap-3 max-lg:flex-col max-lg:items-stretch">
							<div>
								<p class="mb-3 text-xs font-bold uppercase tracking-widest text-accent">Model aliases</p>
								<h2>Create public model names</h2>
							</div>
							<span class="text-ink-soft">{{ modelAliasCount }} alias{{ modelAliasCount === 1 ? '' : 'es' }}</span>
						</div>

						<div class="grid gap-4.5 lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
							<article data-anchor="model-alias-form" class="grid gap-4 rounded-card border border-line bg-surface-strong p-4.5">
								<div>
									<h3>{{ isEditingModelAlias ? 'Update alias' : 'Add alias' }}</h3>
									<p class="mt-1.5 leading-[1.6] text-ink-soft">
										Create a new public model name and point it at a model, optionally locking it to one provider.
									</p>
								</div>

								<form class="grid gap-4" @submit.prevent="submitModelAlias">
									<label class="grid gap-2">
										<span class="text-[0.92rem] font-bold text-ink-strong">Alias model name</span>
										<input
											v-model.trim="modelAliasForm.aliasModelId"
											class="w-full rounded-(--radius-field) border border-line-strong bg-white/90 px-4 py-3.75 text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
											type="text"
											autocomplete="off"
											placeholder="team-gateway"
											required
										/>
									</label>

									<label class="grid gap-2">
										<span class="text-[0.92rem] font-bold text-ink-strong">Target model</span>
										<input
											v-model.trim="modelAliasForm.targetModelId"
											class="w-full rounded-(--radius-field) border border-line-strong bg-white/90 px-4 py-3.75 text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
											list="model-alias-target-models"
											type="text"
											autocomplete="off"
											placeholder="gpt-4.1"
											required
										/>
										<datalist id="model-alias-target-models">
											<option v-for="modelId in targetModelOptions" :key="modelId" :value="modelId" />
										</datalist>
									</label>

									<label class="grid gap-2">
										<span class="text-[0.92rem] font-bold text-ink-strong">Target provider (optional)</span>
										<select
											v-model="modelAliasForm.targetProviderId"
											class="w-full rounded-(--radius-field) border border-line-strong bg-white/90 px-4 py-3.75 text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
										>
											<option value="">Any enabled provider</option>
											<option v-for="provider in aliasTargetProviderOptions" :key="provider.id" :value="String(provider.id)">
												{{ provider.name }}{{ provider.enabled ? '' : ' (disabled)' }}
											</option>
										</select>
										<small class="text-ink-soft">Leave blank to route through any enabled provider that serves the target model.</small>
									</label>

									<div class="flex flex-wrap items-center justify-between gap-3 max-lg:flex-col max-lg:items-stretch">
										<button
											class="inline-flex min-h-12 items-center justify-center rounded-full border border-transparent bg-[linear-gradient(135deg,var(--color-accent),#0f9275)] px-4.5 font-bold text-[#f7fffc] transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
											type="submit"
											:disabled="aliasSaving"
										>
											{{ aliasSaving ? 'Saving…' : isEditingModelAlias ? 'Update alias' : 'Create alias' }}
										</button>
										<button
											v-if="isEditingModelAlias"
											class="inline-flex min-h-12 items-center justify-center rounded-full border border-line bg-[rgba(255,255,255,0.72)] px-4.5 font-bold text-ink-strong transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
											type="button"
											@click="resetModelAliasForm"
										>
											Cancel edit
										</button>
									</div>
								</form>
							</article>

							<article data-anchor="model-alias-list" class="flex flex-col gap-4 rounded-card border border-line bg-surface-strong p-4.5">
								<div>
									<h3>Configured aliases</h3>
									<p class="mt-1.5 leading-[1.6] text-ink-soft">
										These aliases are exposed by `/v1/models` and are available to clients as if they were native model names.
									</p>
								</div>

								<p
									v-if="modelAliases.length === 0"
									class="rounded-[16px] border border-dashed border-line bg-white/50 px-3.5 py-4 leading-[1.6] text-ink-soft"
								>
									No aliases have been configured yet.
								</p>

								<div v-else class="grid gap-3">
									<ModelAliasCard
										v-for="alias in modelAliases"
										:key="alias.id"
										:alias="alias"
										:editing="editingModelAliasId === alias.id"
										@edit="beginEditModelAlias(alias)"
										@delete="removeModelAlias(alias)"
									/>
								</div>
							</article>
						</div>
					</section>

					<section data-anchor="providers" class="rounded-panel border border-line bg-surface p-5 shadow-panel backdrop-blur-[18px] lg:p-7">
						<div class="mb-5 flex items-start justify-between gap-3">
							<div>
								<p class="mb-3 text-xs font-bold uppercase tracking-widest text-accent">Providers</p>
								<h2>Routing inventory</h2>
							</div>
							<span class="text-ink-soft">{{ enabledProviderCount }} active / {{ providerCount }} total</span>
						</div>

						<div
							v-if="providers.length === 0"
							class="rounded-card border border-line bg-surface-strong px-5.5 py-6.5 leading-[1.6] text-ink-soft"
						>
							Add a provider above to start discovering models and proxying requests.
						</div>

						<div v-else class="grid gap-4.5 lg:grid-cols-2">
							<ProviderCard
								v-for="provider in providers"
								:key="provider.id"
								:provider="provider"
								:syncing="syncingProviderId === provider.id"
								:pending-rules="pendingModelDisableRules"
								@edit="beginEdit(provider)"
								@sync="syncProvider(provider)"
								@select-rule="toggleModelDisableRule(provider, $event)"
								@delete="removeProvider(provider)"
							/>
						</div>
					</section>

					<section data-anchor="models" class="rounded-panel border border-line bg-surface p-5 shadow-panel backdrop-blur-[18px] lg:p-7">
						<div class="mb-5 flex items-start justify-between gap-3">
							<div>
								<p class="mb-3 text-xs font-bold uppercase tracking-widest text-accent">Model catalog</p>
								<h2>Aggregated routing table</h2>
							</div>
							<span class="text-ink-soft">{{ modelCount }} routable models</span>
						</div>

						<div v-if="models.length === 0" class="rounded-card border border-line bg-surface-strong px-5.5 py-6.5 leading-[1.6] text-ink-soft">
							Sync at least one provider catalog to populate the gateway&apos;s model routes.
						</div>

						<div v-else class="grid gap-4.5 lg:grid-cols-3">
							<ModelCard
								v-for="model in models"
								:key="model.id"
								:model="model"
								:pending-rules="pendingModelDisableRules"
								@select-rule="toggleModelDisableRule($event, model.id)"
							/>
						</div>
					</section>

					<section data-anchor="request-logs" class="rounded-panel border border-line bg-surface p-5 shadow-panel backdrop-blur-[18px] lg:p-7">
						<div class="mb-5 flex items-start justify-between gap-3 max-lg:flex-col max-lg:items-stretch">
							<div>
								<p class="mb-3 text-xs font-bold uppercase tracking-widest text-accent">Request audit</p>
								<h2>Recent gateway traffic</h2>
							</div>
							<span class="text-ink-soft">{{ requestLogCount }} recent rows</span>
						</div>

						<div data-anchor="request-log-clear" class="mb-5 grid gap-4 rounded-card border border-line bg-surface-strong p-4.5">
							<div>
								<h3>Clear logs</h3>
								<p class="mt-1.5 leading-[1.6] text-ink-soft">Delete request history by provider, by requested-at range, or both.</p>
							</div>

							<div class="grid gap-4 md:grid-cols-3">
								<label class="grid gap-2">
									<span class="text-[0.92rem] font-bold text-ink-strong">Provider</span>
									<select
										v-model="clearRequestLogsForm.providerId"
										class="w-full rounded-(--radius-field) border border-line-strong bg-white/90 px-4 py-3.75 text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
									>
										<option value="">All providers</option>
										<option v-for="provider in providers" :key="provider.id" :value="String(provider.id)">
											{{ provider.name }}
										</option>
									</select>
								</label>

								<label class="grid gap-2">
									<span class="text-[0.92rem] font-bold text-ink-strong">From</span>
									<input
										v-model="clearRequestLogsForm.from"
										class="w-full rounded-(--radius-field) border border-line-strong bg-white/90 px-4 py-3.75 text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
										type="datetime-local"
										step="60"
									/>
								</label>

								<label class="grid gap-2">
									<span class="text-[0.92rem] font-bold text-ink-strong">To</span>
									<input
										v-model="clearRequestLogsForm.to"
										class="w-full rounded-(--radius-field) border border-line-strong bg-white/90 px-4 py-3.75 text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
										type="datetime-local"
										step="60"
									/>
								</label>
							</div>

							<div class="flex flex-wrap items-center justify-between gap-3 max-lg:flex-col max-lg:items-stretch">
								<button
									class="inline-flex min-h-12 items-center justify-center rounded-full border border-line bg-[rgba(255,255,255,0.72)] px-4.5 font-bold text-ink-strong transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
									type="button"
									@click="resetRequestLogFilters"
								>
									Reset filters
								</button>
								<button
									class="inline-flex min-h-12 items-center justify-center rounded-full border border-[rgba(164,63,63,0.2)] bg-[rgba(255,255,255,0.72)] px-4.5 font-bold text-danger transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
									type="button"
									:disabled="clearingLogs"
									@click="clearLogs"
								>
									{{ clearingLogs ? 'Clearing…' : 'Clear matching logs' }}
								</button>
							</div>
						</div>

						<div
							v-if="requestLogs.length === 0"
							class="rounded-card border border-line bg-surface-strong px-5.5 py-6.5 leading-[1.6] text-ink-soft"
						>
							No gateway requests have been recorded yet.
						</div>

						<div v-else class="relative">
							<div ref="requestLogList" class="grid max-h-[80vh] gap-2 overflow-y-auto pb-8 pr-1 pt-1" @scroll="syncRequestLogScrollCue">
								<RequestLogCard v-for="requestLog in requestLogs" :key="requestLog.id" :request-log="requestLog" />
							</div>
							<div
								v-if="showRequestLogScrollCue"
								class="pointer-events-none absolute inset-x-0 bottom-0 z-10 h-18 rounded-b-card bg-linear-to-t from-surface-strong via-[rgba(255,252,247,0.74)] to-transparent"
							/>
							<div v-if="showRequestLogScrollCue" class="pointer-events-none absolute inset-x-0 bottom-3 z-20 flex justify-center">
								<div
									class="inline-flex items-center gap-2 rounded-full border border-line bg-[rgba(255,252,247,0.92)] px-3 py-1.5 text-[0.74rem] font-bold uppercase tracking-[0.16em] text-accent shadow-[0_10px_20px_rgba(24,34,47,0.08)] backdrop-blur-sm"
								>
									<ChevronsDown class="size-4" />
									Scroll for more
								</div>
							</div>
						</div>
					</section>
				</div>
			</div>
		</template>
	</div>
</template>
