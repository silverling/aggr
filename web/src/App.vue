<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { Toaster, toast } from 'vue-sonner'
import ModelCard from './components/ModelCard.vue'
import ModelAliasCard from './components/ModelAliasCard.vue'
import ProviderCard from './components/ProviderCard.vue'
import RequestLogCard from './components/RequestLogCard.vue'
import StatCard from './components/StatCard.vue'
import type {
	DeleteProxyRequestsPayload,
	ModelAliasView,
	ModelAliasesPayload,
	ModelDisableRuleSelection,
	ModelRoute,
	ModelsPayload,
	NoticeTone,
	ProviderView,
	ProvidersPayload,
	ProxyRequestLogView,
	ProxyRequestsPayload,
	SetModelDisableRulePayload,
} from './types'

const providers = ref<ProviderView[]>([])
const models = ref<ModelRoute[]>([])
const modelAliases = ref<ModelAliasView[]>([])
const requestLogs = ref<ProxyRequestLogView[]>([])
const loading = ref(false)
const saving = ref(false)
const aliasSaving = ref(false)
const syncingAll = ref(false)
const syncingProviderId = ref<number | null>(null)
const editingProviderId = ref<number | null>(null)
const editingModelAliasId = ref<number | null>(null)
const applyingModelDisableRule = ref(false)
const clearingLogs = ref(false)
const selectedModelDisableRule = ref<ModelDisableRuleSelection | null>(null)
const requestLogLimit = 40

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
const isEditing = computed(() => editingProviderId.value !== null)
const isEditingModelAlias = computed(() => editingModelAliasId.value !== null)
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
const selectedModelDisableRuleProvider = computed(() => {
	if (selectedModelDisableRule.value === null) {
		return null
	}

	return providers.value.find((provider) => provider.id === selectedModelDisableRule.value?.providerId) ?? null
})
const selectedModelDisableRuleExists = computed(() => {
	const selection = selectedModelDisableRule.value
	const provider = selectedModelDisableRuleProvider.value
	if (selection === null || provider === null) {
		return false
	}

	return provider.disabledModels.includes(selection.modelId)
})
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

function reconcileSelectedModelDisableRule() {
	const selection = selectedModelDisableRule.value
	if (selection === null) {
		return
	}

	const provider = providers.value.find((candidate) => candidate.id === selection.providerId)
	if (provider === undefined || !provider.models.includes(selection.modelId)) {
		selectedModelDisableRule.value = null
		return
	}

	selectedModelDisableRule.value = {
		providerId: provider.id,
		providerName: provider.name,
		modelId: selection.modelId,
	}
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

async function request<T>(input: RequestInfo, init?: RequestInit): Promise<T> {
	const response = await fetch(input, init)
	const isJSON = response.headers.get('content-type')?.includes('application/json')
	const payload = isJSON ? await response.json() : null

	if (!response.ok) {
		const message = payload && typeof payload.error === 'string' ? payload.error : `${response.status} ${response.statusText}`
		throw new Error(message)
	}

	return payload as T
}

async function loadDashboard(showNotice = false) {
	loading.value = true
	clearNotice()

	try {
		const [providerPayload, modelPayload, aliasPayload, requestPayload] = await Promise.all([
			request<ProvidersPayload>('/api/providers'),
			request<ModelsPayload>('/api/models'),
			request<ModelAliasesPayload>('/api/model-aliases'),
			request<ProxyRequestsPayload>(`/api/requests?limit=${requestLogLimit}`),
		])
		providers.value = providerPayload.providers
		models.value = modelPayload.models
		modelAliases.value = aliasPayload.aliases
		requestLogs.value = requestPayload.requests
		reconcileSelectedModelDisableRule()
		reconcileEditingModelAlias()

		if (showNotice) {
			setNotice('info', 'Dashboard refreshed.')
		}
	} catch (error) {
		setNotice('error', error instanceof Error ? error.message : 'Failed to load dashboard.')
	} finally {
		loading.value = false
	}
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
		setNotice('error', error instanceof Error ? error.message : 'Failed to sync providers.')
	} finally {
		syncingAll.value = false
	}
}

function selectModelDisableRule(provider: Pick<ProviderView, 'id' | 'name'>, modelId: string) {
	selectedModelDisableRule.value = {
		providerId: provider.id,
		providerName: provider.name,
		modelId,
	}
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
		setNotice('error', error instanceof Error ? error.message : `Failed to delete ${alias.aliasModelId}.`)
	}
}

function clearSelectedModelDisableRule() {
	selectedModelDisableRule.value = null
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
	const selection = selectedModelDisableRule.value
	const provider = selectedModelDisableRuleProvider.value
	if (selection === null || provider === null) {
		return
	}

	applyingModelDisableRule.value = true
	clearNotice()

	const nextDisabled = !selectedModelDisableRuleExists.value

	try {
		await request<SetModelDisableRulePayload>('/api/model-disable-rules', {
			method: 'PUT',
			headers: {
				'Content-Type': 'application/json',
			},
			body: JSON.stringify({
				providerId: selection.providerId,
				modelId: selection.modelId,
				disabled: nextDisabled,
			}),
		})
		await loadDashboard()
		setNotice(
			'success',
			nextDisabled
				? `Disabled ${selection.modelId} for ${selection.providerName}.`
				: `Re-enabled ${selection.modelId} for ${selection.providerName}.`,
		)
	} catch (error) {
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
		setNotice('error', error instanceof Error ? error.message : 'Failed to clear request logs.')
	} finally {
		clearingLogs.value = false
	}
}

onMounted(() => {
	loadDashboard()
})
</script>

<template>
	<Toaster richColors position="top-right" />
	<div data-anchor="dashboard" class="mx-auto grid w-[min(1240px,calc(100vw-32px))] gap-[22px] py-8 max-lg:w-[calc(100vw-24px)] max-lg:py-4">
		<header
			data-anchor="hero"
			class="grid gap-7 overflow-hidden rounded-[var(--radius-panel)] border border-line bg-surface p-5 shadow-panel backdrop-blur-[18px] lg:p-[34px]"
		>
			<div class="max-w-[760px]">
				<p class="mb-3 text-xs font-bold uppercase tracking-[0.1em] text-accent">Unified gateway</p>
				<h1>Aggr</h1>
				<p class="mt-4 max-w-[58ch] text-[1.04rem] leading-[1.65] text-ink-soft">
					Store provider credentials in SQLite, discover their model catalogs, and proxy each request to the provider that actually serves the
					requested model.
				</p>
			</div>

			<div class="flex flex-wrap items-center justify-between gap-3 max-lg:flex-col max-lg:items-stretch">
				<button
					class="inline-flex min-h-12 items-center justify-center rounded-full border border-line bg-[rgba(255,255,255,0.72)] px-[18px] font-bold text-ink-strong transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
					type="button"
					:disabled="loading"
					@click="loadDashboard(true)"
				>
					{{ loading ? 'Refreshing…' : 'Refresh dashboard' }}
				</button>
				<button
					class="inline-flex min-h-12 items-center justify-center rounded-full border border-transparent bg-[linear-gradient(135deg,var(--color-accent),#0f9275)] px-[18px] font-bold text-[#f7fffc] transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
					type="button"
					:disabled="syncingAll"
					@click="syncAll"
				>
					{{ syncingAll ? 'Syncing catalogs…' : 'Sync all providers' }}
				</button>
			</div>

			<div class="grid gap-[18px] md:grid-cols-2 xl:grid-cols-4">
				<StatCard label="Providers" :value="providerCount" :description="`${enabledProviderCount} enabled for routing`" />
				<StatCard label="Models" :value="modelCount" description="From synced `/v1/models` catalogs" />
				<StatCard label="Aliases" :value="modelAliasCount" description="Public model names mapped to upstream targets" />
				<StatCard label="Coverage overlap" :value="duplicateCoverageCount" description="Models offered by multiple providers" />
			</div>
		</header>

		<section class="grid gap-[18px] lg:grid-cols-[minmax(0,1.1fr)_minmax(0,0.9fr)]">
			<article
				data-anchor="provider-config"
				class="rounded-[var(--radius-panel)] border border-line bg-surface p-5 shadow-panel backdrop-blur-[18px] lg:p-7"
			>
				<div class="mb-5 flex items-start justify-between gap-3">
					<div>
						<p class="mb-3 text-xs font-bold uppercase tracking-[0.1em] text-accent">Provider config</p>
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
							class="w-full rounded-[var(--radius-field)] border border-line-strong bg-white/90 px-4 py-[15px] text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
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
							class="w-full rounded-[var(--radius-field)] border border-line-strong bg-white/90 px-4 py-[15px] text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
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
							class="w-full rounded-[var(--radius-field)] border border-line-strong bg-white/90 px-4 py-[15px] text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
							type="password"
							:placeholder="isEditing ? 'Leave blank to keep the current key' : 'sk-...'"
							:required="!isEditing"
						/>
					</label>

					<label class="grid gap-2">
						<span class="text-[0.92rem] font-bold text-ink-strong">User agent</span>
						<input
							v-model.trim="form.userAgent"
							class="w-full rounded-[var(--radius-field)] border border-line-strong bg-white/90 px-4 py-[15px] text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
							type="text"
							autocomplete="off"
							placeholder="Aggr/1.0"
						/>
						<small class="text-ink-soft">Leave blank to use the SDK default upstream user agent.</small>
					</label>

					<label class="flex items-center justify-start gap-3 rounded-[var(--radius-field)] border border-line bg-surface-muted px-4 py-[14px]">
						<input v-model="form.enabled" class="h-[18px] w-[18px] accent-accent" type="checkbox" />
						<span class="font-bold text-ink-strong">Enabled for model routing</span>
					</label>

					<button
						class="inline-flex min-h-12 items-center justify-center rounded-full border border-transparent bg-[linear-gradient(135deg,var(--color-accent),#0f9275)] px-[18px] font-bold text-[#f7fffc] transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60"
						type="submit"
						:disabled="saving"
					>
						{{ saving ? 'Saving…' : isEditing ? 'Update provider' : 'Create provider' }}
					</button>
				</form>
			</article>

			<article
				data-anchor="quick-start"
				class="rounded-[var(--radius-panel)] border border-line bg-surface p-5 shadow-panel backdrop-blur-[18px] lg:p-7"
			>
				<div class="mb-5 flex items-start justify-between gap-3">
					<div>
						<p class="mb-3 text-xs font-bold uppercase tracking-[0.1em] text-accent">Quick start</p>
						<h2>Point clients at the gateway</h2>
					</div>
					<button class="border-0 bg-transparent p-0 font-bold text-accent" type="button" @click="copyGatewayBase">Copy base URL</button>
				</div>

				<div
					class="mb-4 flex flex-wrap items-start justify-between gap-3 rounded-[var(--radius-card)] border border-line bg-surface-strong p-[18px]"
				>
					<span class="text-[0.82rem] font-bold uppercase tracking-[0.18em] text-accent-strong">Gateway base</span>
					<code class="break-words text-ink-strong">{{ gatewayBase }}</code>
				</div>

				<pre
					class="m-0 overflow-auto rounded-[var(--radius-card)] border border-line bg-[linear-gradient(180deg,rgba(14,32,41,0.96),rgba(14,32,41,0.88)),radial-gradient(circle_at_top_left,rgba(12,118,98,0.28),transparent_55%)] p-[18px] text-[#dff7f1]"
				><code class="whitespace-pre-wrap break-words text-[0.92rem] leading-[1.75]">{{ curlExample }}</code></pre>

				<ul class="mt-4 grid gap-2.5 pl-4 leading-[1.55] text-ink-soft">
					<li><code class="font-mono text-ink-strong">GET /v1/models</code> returns the aggregated model catalog.</li>
					<li>Requests are routed strictly by the <code class="font-mono text-ink-strong">model</code> field in the JSON payload.</li>
					<li>Providers sync automatically after create or update, and you can resync at any time.</li>
				</ul>
			</article>
		</section>

		<section
			data-anchor="model-disable-rules"
			class="rounded-[var(--radius-panel)] border border-line bg-surface p-5 shadow-panel backdrop-blur-[18px] lg:p-7"
		>
			<div class="mb-5 flex items-start justify-between gap-3 max-lg:flex-col max-lg:items-stretch">
				<div>
					<p class="mb-3 text-xs font-bold uppercase tracking-[0.1em] text-accent">Model disable rules</p>
					<h2>Block one provider for one model</h2>
				</div>
				<span class="text-ink-soft">{{ activeModelDisableRules.length }} active rule{{ activeModelDisableRules.length === 1 ? '' : 's' }}</span>
			</div>

			<div class="grid gap-[18px] lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
				<article data-anchor="model-disable-rule-pending" class="grid gap-4 rounded-card border border-line bg-surface-strong p-4.5">
					<div>
						<h3>Pending change</h3>
						<p class="mt-1.5 leading-[1.6] text-ink-soft">
							Click a provider chip in a model card or a model chip in a provider card, then apply the rule to disable or re-enable that
							specific route.
						</p>
					</div>

					<div v-if="selectedModelDisableRule" class="flex flex-wrap items-center gap-2.5">
						<span
							class="inline-flex items-center rounded-full border border-line bg-white/70 px-3 py-2 font-mono text-[0.82rem] font-bold text-ink-strong"
						>
							{{ selectedModelDisableRule.providerName }}
						</span>
						<span class="text-sm text-ink-soft">for</span>
						<span
							class="inline-flex items-center rounded-full border border-line bg-white/70 px-3 py-2 font-mono text-[0.82rem] font-bold text-ink-strong"
						>
							{{ selectedModelDisableRule.modelId }}
						</span>
					</div>
					<p v-else class="rounded-[16px] border border-dashed border-line bg-white/50 px-3.5 py-4 leading-[1.6] text-ink-soft">
						No provider/model pair is selected yet.
					</p>

					<p
						v-if="selectedModelDisableRule"
						:class="[
							'rounded-[16px] px-3.5 py-3 leading-[1.6]',
							selectedModelDisableRuleExists ? 'bg-danger-soft text-danger' : 'bg-[rgba(12,118,98,0.08)] text-accent',
						]"
					>
						{{
							selectedModelDisableRuleExists
								? 'This provider is currently disabled for the selected model.'
								: 'This provider currently participates in routing for the selected model.'
						}}
					</p>

					<div class="flex flex-wrap items-center justify-between gap-3 max-lg:flex-col max-lg:items-stretch">
						<button
							class="inline-flex min-h-12 items-center justify-center rounded-full border border-transparent bg-[linear-gradient(135deg,var(--color-accent),#0f9275)] px-[18px] font-bold text-[#f7fffc] transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
							type="button"
							:disabled="selectedModelDisableRule === null || applyingModelDisableRule"
							@click="applyModelDisableRule"
						>
							{{ applyingModelDisableRule ? 'Applying…' : selectedModelDisableRuleExists ? 'Remove disable rule' : 'Apply disable rule' }}
						</button>
						<button
							class="inline-flex min-h-12 items-center justify-center rounded-full border border-line bg-[rgba(255,255,255,0.72)] px-[18px] font-bold text-ink-strong transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
							type="button"
							:disabled="selectedModelDisableRule === null"
							@click="clearSelectedModelDisableRule"
						>
							Clear selection
						</button>
					</div>
				</article>

				<article data-anchor="model-disable-rule-active" class="grid gap-4 rounded-card border border-line bg-surface-strong p-4.5">
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
								selectedModelDisableRule?.providerId === rule.providerId && selectedModelDisableRule?.modelId === rule.modelId
									? 'border-[rgba(24,34,47,0.24)] bg-[rgba(24,34,47,0.12)] text-ink-strong shadow-[0_10px_24px_rgba(24,34,47,0.08)]'
									: 'border-[rgba(164,63,63,0.18)] bg-danger-soft text-danger',
							]"
							type="button"
							@click="selectModelDisableRule({ id: rule.providerId, name: rule.providerName }, rule.modelId)"
						>
							{{ rule.providerName }} · {{ rule.modelId }}
						</button>
					</div>
				</article>
			</div>
		</section>

		<section
			data-anchor="model-aliases"
			class="rounded-[var(--radius-panel)] border border-line bg-surface p-5 shadow-panel backdrop-blur-[18px] lg:p-7"
		>
			<div class="mb-5 flex items-start justify-between gap-3 max-lg:flex-col max-lg:items-stretch">
				<div>
					<p class="mb-3 text-xs font-bold uppercase tracking-[0.1em] text-accent">Model aliases</p>
					<h2>Create public model names</h2>
				</div>
				<span class="text-ink-soft">{{ modelAliasCount }} alias{{ modelAliasCount === 1 ? '' : 'es' }}</span>
			</div>

			<div class="grid gap-[18px] lg:grid-cols-[minmax(0,0.9fr)_minmax(0,1.1fr)]">
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
								class="w-full rounded-[var(--radius-field)] border border-line-strong bg-white/90 px-4 py-[15px] text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
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
								class="w-full rounded-[var(--radius-field)] border border-line-strong bg-white/90 px-4 py-[15px] text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
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
								class="w-full rounded-[var(--radius-field)] border border-line-strong bg-white/90 px-4 py-[15px] text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
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
								class="inline-flex min-h-12 items-center justify-center rounded-full border border-transparent bg-[linear-gradient(135deg,var(--color-accent),#0f9275)] px-[18px] font-bold text-[#f7fffc] transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
								type="submit"
								:disabled="aliasSaving"
							>
								{{ aliasSaving ? 'Saving…' : isEditingModelAlias ? 'Update alias' : 'Create alias' }}
							</button>
							<button
								v-if="isEditingModelAlias"
								class="inline-flex min-h-12 items-center justify-center rounded-full border border-line bg-[rgba(255,255,255,0.72)] px-[18px] font-bold text-ink-strong transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
								type="button"
								@click="resetModelAliasForm"
							>
								Cancel edit
							</button>
						</div>
					</form>
				</article>

				<article data-anchor="model-alias-list" class="grid gap-4 rounded-card border border-line bg-surface-strong p-4.5">
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

		<section
			data-anchor="providers"
			class="rounded-[var(--radius-panel)] border border-line bg-surface p-5 shadow-panel backdrop-blur-[18px] lg:p-7"
		>
			<div class="mb-5 flex items-start justify-between gap-3">
				<div>
					<p class="mb-3 text-xs font-bold uppercase tracking-[0.1em] text-accent">Providers</p>
					<h2>Routing inventory</h2>
				</div>
				<span class="text-ink-soft">{{ enabledProviderCount }} active / {{ providerCount }} total</span>
			</div>

			<div
				v-if="providers.length === 0"
				class="rounded-[var(--radius-card)] border border-line bg-surface-strong px-[22px] py-[26px] leading-[1.6] text-ink-soft"
			>
				Add a provider above to start discovering models and proxying requests.
			</div>

			<div v-else class="grid gap-[18px] lg:grid-cols-2">
				<ProviderCard
					v-for="provider in providers"
					:key="provider.id"
					:provider="provider"
					:syncing="syncingProviderId === provider.id"
					:selected-rule="selectedModelDisableRule"
					@edit="beginEdit(provider)"
					@sync="syncProvider(provider)"
					@select-rule="selectModelDisableRule(provider, $event)"
					@delete="removeProvider(provider)"
				/>
			</div>
		</section>

		<section
			data-anchor="models"
			class="rounded-[var(--radius-panel)] border border-line bg-surface p-5 shadow-panel backdrop-blur-[18px] lg:p-7"
		>
			<div class="mb-5 flex items-start justify-between gap-3">
				<div>
					<p class="mb-3 text-xs font-bold uppercase tracking-[0.1em] text-accent">Model catalog</p>
					<h2>Aggregated routing table</h2>
				</div>
				<span class="text-ink-soft">{{ modelCount }} routable models</span>
			</div>

			<div
				v-if="models.length === 0"
				class="rounded-[var(--radius-card)] border border-line bg-surface-strong px-[22px] py-[26px] leading-[1.6] text-ink-soft"
			>
				Sync at least one provider catalog to populate the gateway&apos;s model routes.
			</div>

			<div v-else class="grid gap-[18px] lg:grid-cols-3">
				<ModelCard
					v-for="model in models"
					:key="model.id"
					:model="model"
					:selected-rule="selectedModelDisableRule"
					@select-rule="selectModelDisableRule($event, model.id)"
				/>
			</div>
		</section>

		<section
			data-anchor="request-logs"
			class="rounded-[var(--radius-panel)] border border-line bg-surface p-5 shadow-panel backdrop-blur-[18px] lg:p-7"
		>
			<div class="mb-5 flex items-start justify-between gap-3 max-lg:flex-col max-lg:items-stretch">
				<div>
					<p class="mb-3 text-xs font-bold uppercase tracking-[0.1em] text-accent">Request audit</p>
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
							class="w-full rounded-[var(--radius-field)] border border-line-strong bg-white/90 px-4 py-[15px] text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
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
							class="w-full rounded-[var(--radius-field)] border border-line-strong bg-white/90 px-4 py-[15px] text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
							type="datetime-local"
							step="60"
						/>
					</label>

					<label class="grid gap-2">
						<span class="text-[0.92rem] font-bold text-ink-strong">To</span>
						<input
							v-model="clearRequestLogsForm.to"
							class="w-full rounded-[var(--radius-field)] border border-line-strong bg-white/90 px-4 py-[15px] text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
							type="datetime-local"
							step="60"
						/>
					</label>
				</div>

				<div class="flex flex-wrap items-center justify-between gap-3 max-lg:flex-col max-lg:items-stretch">
					<button
						class="inline-flex min-h-12 items-center justify-center rounded-full border border-line bg-[rgba(255,255,255,0.72)] px-[18px] font-bold text-ink-strong transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
						type="button"
						@click="resetRequestLogFilters"
					>
						Reset filters
					</button>
					<button
						class="inline-flex min-h-12 items-center justify-center rounded-full border border-[rgba(164,63,63,0.2)] bg-[rgba(255,255,255,0.72)] px-[18px] font-bold text-danger transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
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
				class="rounded-[var(--radius-card)] border border-line bg-surface-strong px-[22px] py-[26px] leading-[1.6] text-ink-soft"
			>
				No gateway requests have been recorded yet.
			</div>

			<div v-else class="grid gap-[18px]">
				<RequestLogCard v-for="requestLog in requestLogs" :key="requestLog.id" :request-log="requestLog" />
			</div>
		</section>
	</div>
</template>
