<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'

interface Provider {
	id: number
	name: string
	baseUrl: string
	enabled: boolean
	models: string[]
	lastError?: string
	lastSyncedAt?: string
	apiKeyConfigured: boolean
	apiKeyPreview?: string
}

interface ModelRoute {
	id: string
	providers: Array<{
		id: number
		name: string
	}>
}

interface ProvidersPayload {
	providers: Provider[]
}

interface ModelsPayload {
	models: ModelRoute[]
}

type NoticeTone = 'success' | 'error' | 'info'

const providers = ref<Provider[]>([])
const models = ref<ModelRoute[]>([])
const loading = ref(false)
const saving = ref(false)
const syncingAll = ref(false)
const syncingProviderId = ref<number | null>(null)
const editingProviderId = ref<number | null>(null)
const notice = ref<{ tone: NoticeTone; text: string } | null>(null)

const form = reactive({
	name: '',
	baseUrl: 'https://api.openai.com/v1',
	apiKey: '',
	enabled: true,
})

const dateFormatter = new Intl.DateTimeFormat(undefined, {
	dateStyle: 'medium',
	timeStyle: 'short',
})

const gatewayBase = computed(() => `${window.location.origin}/v1`)
const providerCount = computed(() => providers.value.length)
const enabledProviderCount = computed(() => providers.value.filter((provider) => provider.enabled).length)
const modelCount = computed(() => models.value.length)
const duplicateCoverageCount = computed(() => models.value.filter((model) => model.providers.length > 1).length)
const isEditing = computed(() => editingProviderId.value !== null)
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
	notice.value = { tone, text }
}

function clearNotice() {
	notice.value = null
}

function resetForm() {
	editingProviderId.value = null
	form.name = ''
	form.baseUrl = 'https://api.openai.com/v1'
	form.apiKey = ''
	form.enabled = true
}

function formatTimestamp(value?: string) {
	if (!value) {
		return 'Not synced yet'
	}

	const parsed = new Date(value)
	if (Number.isNaN(parsed.valueOf())) {
		return value
	}

	return dateFormatter.format(parsed)
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
		const [providerPayload, modelPayload] = await Promise.all([
			request<ProvidersPayload>('/api/providers'),
			request<ModelsPayload>('/api/models'),
		])
		providers.value = providerPayload.providers
		models.value = modelPayload.models

		if (showNotice) {
			setNotice('info', 'Dashboard refreshed.')
		}
	} catch (error) {
		setNotice('error', error instanceof Error ? error.message : 'Failed to load dashboard.')
	} finally {
		loading.value = false
	}
}

function beginEdit(provider: Provider) {
	editingProviderId.value = provider.id
	form.name = provider.name
	form.baseUrl = provider.baseUrl
	form.apiKey = ''
	form.enabled = provider.enabled
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

async function syncProvider(provider: Provider) {
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

async function removeProvider(provider: Provider) {
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

async function copyGatewayBase() {
	try {
		await navigator.clipboard.writeText(gatewayBase.value)
		setNotice('success', 'Gateway base copied to the clipboard.')
	} catch {
		setNotice('error', 'Clipboard access is unavailable in this browser.')
	}
}

onMounted(() => {
	loadDashboard()
})
</script>

<template>
	<div class="shell">
		<header class="hero panel">
			<div class="hero-copy">
				<p class="eyebrow">OpenAI-compatible gateway</p>
				<h1>One `/v1` endpoint, many upstream model providers.</h1>
				<p class="lede">
					Store provider credentials in SQLite, discover their model catalogs, and proxy each request to the provider that actually serves the
					requested model.
				</p>
			</div>

			<div class="hero-actions">
				<button class="button subtle" type="button" :disabled="loading" @click="loadDashboard(true)">
					{{ loading ? 'Refreshing…' : 'Refresh dashboard' }}
				</button>
				<button class="button primary" type="button" :disabled="syncingAll" @click="syncAll">
					{{ syncingAll ? 'Syncing catalogs…' : 'Sync all providers' }}
				</button>
			</div>

			<div class="stats-grid">
				<article class="stat-card">
					<p>Providers</p>
					<strong>{{ providerCount }}</strong>
					<span>{{ enabledProviderCount }} enabled for routing</span>
				</article>
				<article class="stat-card">
					<p>Models</p>
					<strong>{{ modelCount }}</strong>
					<span>From synced `/v1/models` catalogs</span>
				</article>
				<article class="stat-card">
					<p>Coverage overlap</p>
					<strong>{{ duplicateCoverageCount }}</strong>
					<span>Models offered by multiple providers</span>
				</article>
			</div>
		</header>

		<div v-if="notice" :class="['notice', notice.tone]">
			{{ notice.text }}
		</div>

		<section class="top-grid">
			<article class="panel">
				<div class="section-head">
					<div>
						<p class="eyebrow">Provider config</p>
						<h2>{{ isEditing ? 'Update an upstream provider' : 'Add an upstream provider' }}</h2>
					</div>
					<button v-if="isEditing" class="text-button" type="button" @click="resetForm">Cancel edit</button>
				</div>

				<form class="provider-form" @submit.prevent="submitProvider">
					<label>
						<span>Display name</span>
						<input v-model.trim="form.name" type="text" autocomplete="off" placeholder="OpenAI primary" required />
					</label>

					<label>
						<span>Base URL</span>
						<input v-model.trim="form.baseUrl" type="url" autocomplete="off" placeholder="https://api.openai.com/v1" required />
						<small>Use the provider&apos;s OpenAI-compatible API root.</small>
					</label>

					<label>
						<span>API key</span>
						<input
							v-model.trim="form.apiKey"
							type="password"
							:placeholder="isEditing ? 'Leave blank to keep the current key' : 'sk-...'"
							:required="!isEditing"
						/>
					</label>

					<label class="checkbox">
						<input v-model="form.enabled" type="checkbox" />
						<span>Enabled for model routing</span>
					</label>

					<button class="button primary" type="submit" :disabled="saving">
						{{ saving ? 'Saving…' : isEditing ? 'Update provider' : 'Create provider' }}
					</button>
				</form>
			</article>

			<article class="panel">
				<div class="section-head">
					<div>
						<p class="eyebrow">Quick start</p>
						<h2>Point clients at the gateway</h2>
					</div>
					<button class="text-button" type="button" @click="copyGatewayBase">Copy base URL</button>
				</div>

				<div class="endpoint-card">
					<span class="endpoint-label">Gateway base</span>
					<code>{{ gatewayBase }}</code>
				</div>

				<pre class="snippet"><code>{{ curlExample }}</code></pre>

				<ul class="tips">
					<li>`GET /v1/models` returns the aggregated model catalog.</li>
					<li>Requests are routed strictly by the `model` field in the JSON payload.</li>
					<li>Providers sync automatically after create or update, and you can resync at any time.</li>
				</ul>
			</article>
		</section>

		<section class="panel panel-body">
			<div class="section-head">
				<div>
					<p class="eyebrow">Providers</p>
					<h2>Routing inventory</h2>
				</div>
				<span class="section-meta">{{ enabledProviderCount }} active / {{ providerCount }} total</span>
			</div>

			<div v-if="providers.length === 0" class="empty-state">Add a provider above to start discovering models and proxying requests.</div>

			<div v-else class="provider-grid">
				<article v-for="provider in providers" :key="provider.id" class="provider-card">
					<div class="provider-topline">
						<div>
							<h3>{{ provider.name }}</h3>
							<p class="provider-url">{{ provider.baseUrl }}</p>
						</div>
						<span :class="['badge', provider.enabled ? 'enabled' : 'disabled']">
							{{ provider.enabled ? 'Enabled' : 'Disabled' }}
						</span>
					</div>

					<div class="provider-meta">
						<span>{{ provider.apiKeyConfigured ? provider.apiKeyPreview : 'No API key' }}</span>
						<span>{{ formatTimestamp(provider.lastSyncedAt) }}</span>
					</div>

					<p v-if="provider.lastError" class="provider-error">{{ provider.lastError }}</p>

					<div v-if="provider.models.length > 0" class="chip-row">
						<span v-for="model in provider.models" :key="model" class="chip">
							{{ model }}
						</span>
					</div>
					<p v-else class="provider-hint">No models synced yet.</p>

					<div class="provider-actions">
						<button class="button subtle" type="button" @click="beginEdit(provider)">Edit</button>
						<button class="button subtle" type="button" :disabled="syncingProviderId === provider.id" @click="syncProvider(provider)">
							{{ syncingProviderId === provider.id ? 'Syncing…' : 'Sync models' }}
						</button>
						<button class="button danger" type="button" @click="removeProvider(provider)">Delete</button>
					</div>
				</article>
			</div>
		</section>

		<section class="panel panel-body">
			<div class="section-head">
				<div>
					<p class="eyebrow">Model catalog</p>
					<h2>Aggregated routing table</h2>
				</div>
				<span class="section-meta">{{ modelCount }} routable models</span>
			</div>

			<div v-if="models.length === 0" class="empty-state">Sync at least one provider catalog to populate the gateway&apos;s model routes.</div>

			<div v-else class="model-grid">
				<article v-for="model in models" :key="model.id" class="model-card">
					<h3>{{ model.id }}</h3>
					<div class="chip-row compact">
						<span v-for="provider in model.providers" :key="provider.id" class="chip provider-chip">
							{{ provider.name }}
						</span>
					</div>
				</article>
			</div>
		</section>
	</div>
</template>
