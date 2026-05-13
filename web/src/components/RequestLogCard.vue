<script setup lang="ts">
import { formatDuration } from '../lib/utils'
import type { ProxyRequestLogView } from '../types'
import { Package, Building2, Clock, ClockArrowUp } from '@lucide/vue'
import { computed } from 'vue'

const props = defineProps<{
	requestLog: ProxyRequestLogView
}>()

const dateFormatter = new Intl.DateTimeFormat(undefined, {
	dateStyle: 'medium',
	timeStyle: 'medium',
})

const statusClass = computed(() => {
	const status = props.requestLog.receivedResponse.status
	if (status === undefined) {
		return 'border border-line bg-surface-muted text-ink-soft'
	}
	if (status >= 500) {
		return 'border border-[rgba(164,63,63,0.18)] bg-danger-soft text-danger'
	}
	if (status >= 400) {
		return 'border border-[rgba(200,93,53,0.18)] bg-[rgba(200,93,53,0.08)] text-accent-strong'
	}
	return 'border border-accent-soft bg-accent-soft text-accent'
})

function formatTimestamp(value?: string) {
	if (!value) {
		return 'Pending'
	}

	const parsed = new Date(value)
	if (Number.isNaN(parsed.valueOf())) {
		return value
	}

	return dateFormatter.format(parsed)
}

function formatPath(path: string, rawQuery?: string) {
	if (!rawQuery) {
		return path
	}

	return `${path}?${rawQuery}`
}

function formatBody(value?: string, truncated?: boolean, headers?: string) {
	if (!value) {
		return '(empty)'
	}

	const formatted = formatBodyByContentType(value, headers)
	return truncated ? `${formatted}\n\n[truncated]` : formatted
}

function parseAuditHeaders(value?: string) {
	if (!value) {
		return null
	}

	try {
		const parsed = JSON.parse(value) as unknown
		if (!parsed || Array.isArray(parsed) || typeof parsed !== 'object') {
			return null
		}

		return parsed as Record<string, unknown>
	} catch {
		return null
	}
}

function contentTypeFromHeaders(value?: string) {
	const headers = parseAuditHeaders(value)
	if (!headers) {
		return ''
	}

	for (const [key, entry] of Object.entries(headers)) {
		if (key.toLowerCase() !== 'content-type') {
			continue
		}

		if (typeof entry === 'string' && entry.trim()) {
			return entry.split(';', 1)[0].trim().toLowerCase()
		}

		if (Array.isArray(entry)) {
			for (const item of entry) {
				if (typeof item === 'string' && item.trim()) {
					return item.split(';', 1)[0].trim().toLowerCase()
				}
			}
		}
	}

	return ''
}

function isJSONContentType(value: string) {
	return value === 'application/json' || value.endsWith('+json')
}

function prettyJSONString(value: string) {
	try {
		return JSON.stringify(JSON.parse(value), null, 2)
	} catch {
		return value
	}
}

function formatEventStreamBody(value: string) {
	const normalized = value.replaceAll('\r\n', '\n').replaceAll('\r', '\n')

	return normalized
		.split('\n')
		.map((line) => {
			if (!line.startsWith('data:')) {
				return line
			}

			const payload = line.slice(5).trimStart()
			if (!payload || payload === '[DONE]') {
				return line
			}

			const prettyPayload = prettyJSONString(payload)
			if (prettyPayload === payload) {
				return line
			}

			return `data: ${prettyPayload}`
		})
		.join('\n')
}

function formatBodyByContentType(value: string, headers?: string) {
	const contentType = contentTypeFromHeaders(headers)
	if (isJSONContentType(contentType)) {
		return prettyJSONString(value)
	}
	if (contentType === 'text/event-stream') {
		return formatEventStreamBody(value)
	}

	return value
}

function formatHeaders(value?: string) {
	if (!value) {
		return '(empty)'
	}

	const parsed = parseAuditHeaders(value)
	if (!parsed) {
		return value
	}

	const normalized = Object.fromEntries(
		Object.entries(parsed).map(([key, entry]) => {
			if (Array.isArray(entry) && entry.length === 1) {
				return [key, entry[0]]
			}

			return [key, entry]
		}),
	)

	return JSON.stringify(normalized, null, 2)
}
</script>

<template>
	<details
		data-anchor="request-log-card"
		class="group rounded-card border border-line bg-surface-strong p-2 transition duration-150 ease-out hover:-translate-y-px hover:border-[rgba(12,118,98,0.18)] hover:shadow-sm"
	>
		<summary class="list-none cursor-pointer transition duration-150 ease-out">
			<div class="flex flex-wrap items-start justify-between gap-3">
				<div class="grid gap-8 grid-cols-[auto_1fr]">
					<div class="flex flex-wrap items-center gap-2.5">
						<span
							class="inline-flex items-center rounded-full border border-line bg-[rgba(255,255,255,0.72)] px-3 py-1.5 font-mono text-[0.78rem] font-bold uppercase tracking-[0.18em] text-ink-strong transition duration-150 ease-out group-hover:border-[rgba(12,118,98,0.2)] group-hover:bg-white"
						>
							{{ props.requestLog.receivedRequest.method }}
						</span>
						<code class="wrap-break-word text-[0.92rem] text-ink-strong transition duration-150 ease-out group-hover:text-accent min-w-38">
							{{ formatPath(props.requestLog.receivedRequest.path, props.requestLog.receivedRequest.rawQuery) }}
						</code>
					</div>

					<div
						class="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-sm text-ink-soft [&>span]:inline-flex [&>span]:items-center [&>span]:gap-1 font-mono"
					>
						<span><ClockArrowUp class="inline size-4" /> {{ formatTimestamp(props.requestLog.requestedAt) }}</span>
						<span v-if="props.requestLog.providerName"><Building2 class="inline size-4" /> {{ props.requestLog.providerName }}</span>
						<span v-if="props.requestLog.modelId"><Package class="inline size-4" /> {{ props.requestLog.modelId }}</span>
						<span v-if="props.requestLog.durationMs !== undefined">
							<Clock class="inline size-4" />
							{{ formatDuration(props.requestLog.durationMs) }}
						</span>
					</div>
				</div>

				<span
					:class="[
						'inline-flex items-center rounded-full px-3 py-1.5 font-mono text-[0.8rem] font-bold uppercase tracking-[0.12em]',
						statusClass,
					]"
				>
					{{ props.requestLog.receivedResponse.status ?? 'Pending' }}
				</span>
			</div>
		</summary>

		<div class="mt-3 grid gap-4 border-t border-line pt-3">
			<p v-if="props.requestLog.receivedResponse.error" class="rounded-[14px] bg-danger-soft px-3.5 py-3 leading-[1.55] text-danger">
				{{ props.requestLog.receivedResponse.error }}
			</p>

			<div class="grid xl:grid-cols-3">
				<section
					data-anchor="request-log-received-request"
					class="flex flex-col h-180 gap-3 overflow-y-auto border-r border-line bg-surface p-4"
				>
					<div>
						<h3 class="mb-1 text-xs font-bold uppercase tracking-widest text-accent">Received request</h3>
					</div>

					<code class="wrap-break-word rounded-[16px] border border-line bg-white/70 px-3.5 py-3 text-[0.84rem] text-ink"
						>{{ props.requestLog.receivedRequest.method }}
						{{ formatPath(props.requestLog.receivedRequest.path, props.requestLog.receivedRequest.rawQuery) }}</code
					>

					<div class="grid gap-2">
						<span class="text-sm font-bold text-ink-strong">Headers</span>
						<pre
							class="m-0 overflow-auto rounded-[16px] border border-line bg-white/70 p-3.5 text-[0.84rem] leading-[1.65] text-ink"
						><code>{{ formatHeaders(props.requestLog.receivedRequest.headers) }}</code></pre>
					</div>

					<div class="grid gap-2">
						<span class="text-sm font-bold text-ink-strong">Body</span>
						<pre
							class="m-0 overflow-auto rounded-[16px] border border-line bg-white/70 p-3.5 text-[0.84rem] leading-[1.65] text-ink"
						><code>{{ formatBody(props.requestLog.receivedRequest.body, props.requestLog.receivedRequest.bodyTruncated, props.requestLog.receivedRequest.headers) }}</code></pre>
					</div>
				</section>

				<section data-anchor="request-log-sent-request" class="flex flex-col h-180 gap-3 overflow-y-auto border-r border-line bg-surface p-4">
					<div>
						<h3 class="mb-1 text-xs font-bold uppercase tracking-widest text-accent">Sent request</h3>
					</div>

					<template v-if="props.requestLog.sentRequest">
						<code class="wrap-break-word rounded-[16px] border border-line bg-white/70 px-3.5 py-3 text-[0.84rem] text-ink"
							>{{ props.requestLog.sentRequest.method }} {{ props.requestLog.sentRequest.url }}</code
						>

						<div class="grid gap-2">
							<span class="text-sm font-bold text-ink-strong">Headers</span>
							<pre
								class="m-0 overflow-auto rounded-[16px] border border-line bg-white/70 p-3.5 text-[0.84rem] leading-[1.65] text-ink"
							><code>{{ formatHeaders(props.requestLog.sentRequest.headers) }}</code></pre>
						</div>

						<div class="grid gap-2">
							<span class="text-sm font-bold text-ink-strong">Body</span>
							<pre
								class="m-0 overflow-auto rounded-[16px] border border-line bg-white/70 p-3.5 text-[0.84rem] leading-[1.65] text-ink"
							><code>{{ formatBody(props.requestLog.sentRequest.body, props.requestLog.sentRequest.bodyTruncated, props.requestLog.sentRequest.headers) }}</code></pre>
						</div>
					</template>
					<p v-else class="rounded-[16px] border border-dashed border-line bg-white/50 px-3.5 py-4 leading-[1.6] text-ink-soft">
						No upstream request was sent for this record.
					</p>
				</section>

				<section data-anchor="request-log-received-response" class="flex flex-col h-180 gap-3 overflow-y-auto bg-surface p-4">
					<div>
						<h3 class="mb-1 text-xs font-bold uppercase tracking-widest text-accent">Response</h3>
					</div>

					<div class="grid gap-2">
						<span class="text-sm font-bold text-ink-strong">Headers</span>
						<pre
							class="m-0 overflow-auto rounded-[16px] border border-line bg-white/70 p-3.5 text-[0.84rem] leading-[1.65] text-ink"
						><code>{{ formatHeaders(props.requestLog.receivedResponse.headers) }}</code></pre>
					</div>

					<div class="grid gap-2">
						<span class="text-sm font-bold text-ink-strong">Body</span>
						<pre
							class="m-0 overflow-auto rounded-[16px] border border-line bg-white/70 p-3.5 text-[0.84rem] leading-[1.65] text-ink"
						><code>{{ formatBody(props.requestLog.receivedResponse.body, props.requestLog.receivedResponse.bodyTruncated, props.requestLog.receivedResponse.headers) }}</code></pre>
					</div>
				</section>
			</div>
		</div>
	</details>
</template>
