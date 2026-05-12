<script setup lang="ts">
import type { ProxyRequestLogView } from '../types'
import { Package, Building2, Clock, ClockArrowUp, ClockCheck } from '@lucide/vue'
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

function formatBody(value?: string, truncated?: boolean) {
	if (!value) {
		return '(empty)'
	}

	return truncated ? `${value}\n\n[truncated]` : value
}
</script>

<template>
	<details data-anchor="request-log-card" class="rounded-card border border-line bg-surface-strong p-4.5">
		<summary class="list-none cursor-pointer">
			<div class="flex flex-wrap items-start justify-between gap-3">
				<div class="grid gap-16 grid-cols-[auto_1fr]">
					<div class="flex flex-wrap items-center gap-2.5">
						<span
							class="inline-flex items-center rounded-full border border-line bg-[rgba(255,255,255,0.72)] px-3 py-1.5 font-mono text-[0.78rem] font-bold uppercase tracking-[0.18em] text-ink-strong"
						>
							{{ props.requestLog.receivedRequest.method }}
						</span>
						<code class="wrap-break-word text-[0.92rem] text-ink-strong">{{
							formatPath(props.requestLog.receivedRequest.path, props.requestLog.receivedRequest.rawQuery)
						}}</code>
					</div>

					<div
						class="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-sm text-ink-soft [&>span]:inline-flex [&>span]:items-center [&>span]:gap-1"
					>
						<span v-if="props.requestLog.modelId"><Package class="inline size-4" /> {{ props.requestLog.modelId }}</span>
						<span v-if="props.requestLog.providerName"><Building2 class="inline size-4" /> {{ props.requestLog.providerName }}</span>
						<span><ClockArrowUp class="inline size-4" /> {{ formatTimestamp(props.requestLog.requestedAt) }}</span>
						<span v-if="props.requestLog.completedAt"
							><ClockCheck class="inline size-4" /> {{ formatTimestamp(props.requestLog.completedAt) }}</span
						>
						<span v-if="props.requestLog.durationMs !== undefined"><Clock class="inline size-4" />{{ props.requestLog.durationMs }} ms</span>
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

		<div class="mt-4 grid gap-4 border-t border-line pt-4">
			<p v-if="props.requestLog.receivedResponse.error" class="rounded-[14px] bg-danger-soft px-3.5 py-3 leading-[1.55] text-danger">
				{{ props.requestLog.receivedResponse.error }}
			</p>

			<div class="grid gap-4 xl:grid-cols-3">
				<section
					data-anchor="request-log-received-request"
					class="grid h-136 gap-3 overflow-y-auto rounded-[18px] border border-line bg-surface p-4"
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
							class="m-0 overflow-x-auto rounded-[16px] border border-line bg-white/70 p-3.5 text-[0.84rem] leading-[1.65] text-ink"
						><code>{{ props.requestLog.receivedRequest.headers }}</code></pre>
					</div>

					<div class="grid gap-2">
						<span class="text-sm font-bold text-ink-strong">Body</span>
						<pre
							class="m-0 overflow-x-auto rounded-[16px] border border-line bg-white/70 p-3.5 text-[0.84rem] leading-[1.65] text-ink"
						><code>{{ formatBody(props.requestLog.receivedRequest.body, props.requestLog.receivedRequest.bodyTruncated) }}</code></pre>
					</div>
				</section>

				<section
					data-anchor="request-log-sent-request"
					class="grid h-136 gap-3 overflow-y-auto rounded-[18px] border border-line bg-surface p-4"
				>
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
								class="m-0 overflow-x-auto rounded-[16px] border border-line bg-white/70 p-3.5 text-[0.84rem] leading-[1.65] text-ink"
							><code>{{ props.requestLog.sentRequest.headers }}</code></pre>
						</div>

						<div class="grid gap-2">
							<span class="text-sm font-bold text-ink-strong">Body</span>
							<pre
								class="m-0 overflow-x-auto rounded-[16px] border border-line bg-white/70 p-3.5 text-[0.84rem] leading-[1.65] text-ink"
							><code>{{ formatBody(props.requestLog.sentRequest.body, props.requestLog.sentRequest.bodyTruncated) }}</code></pre>
						</div>
					</template>
					<p v-else class="rounded-[16px] border border-dashed border-line bg-white/50 px-3.5 py-4 leading-[1.6] text-ink-soft">
						No upstream request was sent for this record.
					</p>
				</section>

				<section
					data-anchor="request-log-received-response"
					class="grid h-136 gap-3 overflow-y-auto rounded-[18px] border border-line bg-surface p-4"
				>
					<div>
						<h3 class="mb-1 text-xs font-bold uppercase tracking-widest text-accent">Response</h3>
					</div>

					<div class="grid gap-2">
						<span class="text-sm font-bold text-ink-strong">Headers</span>
						<pre
							class="m-0 overflow-x-auto rounded-[16px] border border-line bg-white/70 p-3.5 text-[0.84rem] leading-[1.65] text-ink"
						><code>{{ formatBody(props.requestLog.receivedResponse.headers) }}</code></pre>
					</div>

					<div class="grid gap-2">
						<span class="text-sm font-bold text-ink-strong">Body</span>
						<pre
							class="m-0 overflow-x-auto rounded-[16px] border border-line bg-white/70 p-3.5 text-[0.84rem] leading-[1.65] text-ink"
						><code>{{ formatBody(props.requestLog.receivedResponse.body, props.requestLog.receivedResponse.bodyTruncated) }}</code></pre>
					</div>
				</section>
			</div>
		</div>
	</details>
</template>
