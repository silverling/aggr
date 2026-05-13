<script setup lang="ts">
import {
	formatCacheRate,
	formatDuration,
	formatPath,
	formatTimestamp,
	formatTokenCount,
	isJSONContentType,
	prettyJSONString,
} from '../lib/utils'
import type { ProxyRequestLogSummaryView, ProxyRequestLogView } from '../types'
import { Package, Building2, Clock, ClockArrowUp, ArrowRight, ArrowLeft, ArrowRightFromLine, RefreshCcw, Ratio } from '@lucide/vue'
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'

const props = defineProps<{
	pageActive: boolean
	requestLog: ProxyRequestLogSummaryView
	loadRequestLogDetail: (id: number) => Promise<ProxyRequestLogView>
}>()

const detail = ref<ProxyRequestLogView | null>(null)
const detailError = ref('')
const detailLoading = ref(false)
const expanded = ref(false)
const cardRoot = ref<HTMLElement | null>(null)
const summaryButton = ref<HTMLButtonElement | null>(null)
const detailRefreshIntervalMs = 5000
const detailTransitionDurationMs = 800
const detailEnterOpacityDurationMs = 1000
const detailLeaveOpacityDurationMs = 800
const scrollTopInsetPx = 16

let detailRefreshTimer: number | null = null
let resizeFollowFrame: number | null = null
let resizeFollowObserver: ResizeObserver | null = null

const summaryError = computed(() => detail.value?.receivedResponse.error ?? props.requestLog.error ?? '')
const summaryStatus = computed(() => detail.value?.receivedResponse.status ?? props.requestLog.status)
const summaryDurationMS = computed(() => detail.value?.durationMs ?? props.requestLog.durationMs)

const statusClass = computed(() => {
	const status = summaryStatus.value
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

async function loadDetail(background = false) {
	if (detailLoading.value) {
		return
	}

	detailLoading.value = true

	try {
		const payload = await props.loadRequestLogDetail(props.requestLog.id)
		detail.value = payload
		detailError.value = ''
	} catch (error) {
		detailError.value =
			error instanceof Error
				? error.message
				: `${background && detail.value !== null ? 'Failed to refresh' : 'Failed to load'} request log #${props.requestLog.id}.`
	} finally {
		detailLoading.value = false
		syncDetailRefreshTimer()
	}
}

function applyCardScroll(behavior: ScrollBehavior) {
	const card = cardRoot.value
	const anchor = summaryButton.value ?? card
	if (card === null || anchor === null) {
		return
	}

	const scrollContainer = findScrollableAncestor(card)
	if (scrollContainer === null) {
		const anchorRect = anchor.getBoundingClientRect()
		window.scrollTo({
			top: Math.max(0, window.scrollY + anchorRect.top - scrollTopInsetPx),
			behavior,
		})
		return
	}

	const containerRect = scrollContainer.getBoundingClientRect()
	const anchorRect = anchor.getBoundingClientRect()
	const targetTop = scrollContainer.scrollTop + (anchorRect.top - containerRect.top) - scrollTopInsetPx
	const maxScrollTop = Math.max(0, scrollContainer.scrollHeight - scrollContainer.clientHeight)

	scrollContainer.scrollTo({
		top: Math.min(Math.max(0, targetTop), maxScrollTop),
		behavior,
	})
}

async function scrollCardIntoVisibleArea(behavior: ScrollBehavior = 'smooth') {
	if (!expanded.value) {
		return
	}

	await waitForNextFrame()
	applyCardScroll(behavior)
}

function findScrollableAncestor(element: HTMLElement) {
	let current = element.parentElement
	while (current) {
		const style = window.getComputedStyle(current)
		const overflowY = style.overflowY
		if ((overflowY === 'auto' || overflowY === 'scroll' || overflowY === 'overlay') && current.scrollHeight > current.clientHeight) {
			return current
		}
		current = current.parentElement
	}

	return null
}

async function waitForNextFrame() {
	await nextTick()
	await new Promise<void>((resolve) => {
		requestAnimationFrame(() => resolve())
	})
}

function stopDetailRefreshTimer() {
	if (detailRefreshTimer !== null) {
		window.clearInterval(detailRefreshTimer)
		detailRefreshTimer = null
	}
}

function stopResizeFollow() {
	if (resizeFollowFrame !== null) {
		cancelAnimationFrame(resizeFollowFrame)
		resizeFollowFrame = null
	}

	if (resizeFollowObserver !== null) {
		resizeFollowObserver.disconnect()
		resizeFollowObserver = null
	}
}

function scheduleResizeFollowScroll() {
	if (resizeFollowFrame !== null) {
		return
	}

	resizeFollowFrame = requestAnimationFrame(() => {
		resizeFollowFrame = null
		if (expanded.value) {
			applyCardScroll('auto')
		}
	})
}

function startResizeFollow() {
	stopResizeFollow()

	const card = cardRoot.value
	if (card === null || typeof ResizeObserver === 'undefined') {
		return
	}

	resizeFollowObserver = new ResizeObserver(() => {
		scheduleResizeFollowScroll()
	})
	resizeFollowObserver.observe(card)
}

function syncDetailRefreshTimer() {
	stopDetailRefreshTimer()
	if (!props.pageActive || !expanded.value || props.requestLog.completedAt !== undefined) {
		return
	}

	detailRefreshTimer = window.setInterval(() => {
		void loadDetail(true)
	}, detailRefreshIntervalMs)
}

function toggleExpanded() {
	expanded.value = !expanded.value

	if (!expanded.value) {
		stopDetailRefreshTimer()
		stopResizeFollow()
		return
	}

	void scrollCardIntoVisibleArea()
	startResizeFollow()

	if (detail.value === null || detailError.value !== '') {
		void loadDetail(false)
		return
	}

	syncDetailRefreshTimer()
}

function setPanelStyles(panel: HTMLElement, height: string, opacity: string, transform: string) {
	panel.style.height = height
	panel.style.opacity = opacity
	panel.style.transform = transform
	panel.style.overflow = 'hidden'
}

function resetPanelStyles(panel: HTMLElement) {
	panel.style.height = ''
	panel.style.opacity = ''
	panel.style.transform = ''
	panel.style.transition = ''
	panel.style.overflow = ''
}

function transitionPanel(
	panel: HTMLElement,
	targetHeight: string,
	targetOpacity: string,
	targetTransform: string,
	opacityDurationMs: number,
	done: () => void,
) {
	panel.style.transition = `height ${detailTransitionDurationMs}ms ease, opacity ${opacityDurationMs}ms ease, transform ${detailTransitionDurationMs}ms ease`
	void panel.offsetHeight

	requestAnimationFrame(() => {
		panel.style.height = targetHeight
		panel.style.opacity = targetOpacity
		panel.style.transform = targetTransform
	})

	const handleTransitionEnd = (event: TransitionEvent) => {
		if (event.target !== panel || event.propertyName !== 'height') {
			return
		}

		panel.removeEventListener('transitionend', handleTransitionEnd)
		resetPanelStyles(panel)
		done()
	}

	panel.addEventListener('transitionend', handleTransitionEnd)
}

function onBeforeDetailEnter(element: Element) {
	const panel = element as HTMLElement
	setPanelStyles(panel, '0px', '0', 'translateY(-6px)')
}

function onDetailEnter(element: Element, done: () => void) {
	const panel = element as HTMLElement
	transitionPanel(panel, `${panel.scrollHeight}px`, '1', 'translateY(0)', detailEnterOpacityDurationMs, done)
}

function onBeforeDetailLeave(element: Element) {
	const panel = element as HTMLElement
	setPanelStyles(panel, `${panel.scrollHeight}px`, '1', 'translateY(0)')
}

function onDetailLeave(element: Element, done: () => void) {
	const panel = element as HTMLElement
	transitionPanel(panel, '0px', '0', 'translateY(-6px)', detailLeaveOpacityDurationMs, done)
}

watch(
	() => props.pageActive,
	(pageActive) => {
		if (!pageActive) {
			stopDetailRefreshTimer()
			return
		}

		if (!expanded.value || detailLoading.value) {
			return
		}

		void loadDetail(detail.value !== null)
	},
)

watch(
	() => [
		props.requestLog.completedAt,
		props.requestLog.status,
		props.requestLog.cachedInputTokens,
		props.requestLog.nonCachedInputTokens,
		props.requestLog.outputTokens,
		props.requestLog.totalTokens,
	],
	() => {
		if (!expanded.value || detail.value === null) {
			syncDetailRefreshTimer()
			return
		}

		if (
			detail.value.completedAt !== props.requestLog.completedAt ||
			detail.value.receivedResponse.status !== props.requestLog.status ||
			detail.value.cachedInputTokens !== props.requestLog.cachedInputTokens ||
			detail.value.nonCachedInputTokens !== props.requestLog.nonCachedInputTokens ||
			detail.value.outputTokens !== props.requestLog.outputTokens ||
			detail.value.totalTokens !== props.requestLog.totalTokens
		) {
			void loadDetail(true)
			return
		}

		syncDetailRefreshTimer()
	},
)

onBeforeUnmount(() => {
	stopDetailRefreshTimer()
	stopResizeFollow()
})
</script>

<template>
	<article
		ref="cardRoot"
		data-anchor="request-log-card"
		class="group rounded-card border border-line bg-surface-strong p-2 transition duration-150 ease-out hover:-translate-y-px hover:border-[rgba(12,118,98,0.18)] hover:shadow-sm"
	>
		<button
			ref="summaryButton"
			class="block w-full cursor-pointer text-left transition duration-150 ease-out"
			type="button"
			:aria-expanded="expanded"
			@click="toggleExpanded"
		>
			<div class="flex flex-wrap items-center gap-3">
				<span :class="['inline-flex items-center rounded-full px-3 py-1.5 font-mono text-[0.8rem] font-bold gap-2', statusClass]">
					<code class="uppercase tracking-widest">{{ summaryStatus ?? 'Pending' }}</code>
					<code class="wrap-break-word">
						{{ formatPath(props.requestLog.path, props.requestLog.rawQuery) }}
					</code>
				</span>

				<div
					class="flex flex-wrap items-center gap-x-4 gap-y-1.5 text-sm text-ink-soft [&>span]:inline-flex [&>span]:items-center [&>span]:gap-1 font-mono"
				>
					<span><ClockArrowUp class="inline size-4" />{{ formatTimestamp(props.requestLog.requestedAt) }}</span>
					<span v-if="props.requestLog.providerName"><Building2 class="inline size-4" />{{ props.requestLog.providerName }}</span>
					<span v-if="props.requestLog.modelId"><Package class="inline size-4" />{{ props.requestLog.modelId }}</span>
					<span v-if="summaryDurationMS !== undefined" class="min-w-16">
						<Clock class="inline size-4" />
						{{ formatDuration(summaryDurationMS) }}
					</span>
				</div>

				<div v-if="summaryStatus?.toString().startsWith('2')" class="flex flex-wrap gap-2 font-mono text-[0.72rem] text-ink-soft">
					<span
						class="rounded-full border border-line bg-white/65 px-3 py-1.5 flex items-center gap-1"
						:title="`Cached ${props.requestLog.cachedInputTokens} tokens`"
					>
						<ArrowRightFromLine class="size-3" /> {{ formatTokenCount(props.requestLog.cachedInputTokens) }}
					</span>
					<span
						class="rounded-full border border-line bg-white/65 px-3 py-1.5 flex items-center gap-1"
						:title="`Non-cached ${props.requestLog.nonCachedInputTokens} tokens`"
					>
						<ArrowRight class="size-3" /> {{ formatTokenCount(props.requestLog.nonCachedInputTokens) }}
					</span>
					<span
						class="rounded-full border border-line bg-white/65 px-3 py-1.5 flex items-center gap-1"
						:title="`Output ${props.requestLog.outputTokens} tokens`"
					>
						<ArrowLeft class="size-3" /> {{ formatTokenCount(props.requestLog.outputTokens) }}
					</span>
					<span
						class="rounded-full border border-line bg-white/65 px-3 py-1.5 flex items-center gap-1"
						:title="`Total ${props.requestLog.totalTokens} tokens`"
					>
						<RefreshCcw class="size-3" /> {{ formatTokenCount(props.requestLog.totalTokens) }}
					</span>
					<span
						class="rounded-full border border-line bg-white/65 px-3 py-1.5 flex items-center gap-1"
						:title="`Cache rate ${formatCacheRate(props.requestLog.cachedInputTokens, props.requestLog.totalTokens)}`"
					>
						<Ratio class="size-3" /> {{ formatCacheRate(props.requestLog.cachedInputTokens, props.requestLog.totalTokens) }}</span
					>
				</div>
			</div>
		</button>

		<Transition
			:css="false"
			@before-enter="onBeforeDetailEnter"
			@enter="onDetailEnter"
			@before-leave="onBeforeDetailLeave"
			@leave="onDetailLeave"
		>
			<div v-if="expanded && !detailLoading" class="mt-3 overflow-hidden">
				<div class="grid gap-4 border-t border-line pt-3">
					<p v-if="summaryError" class="rounded-[14px] bg-danger-soft px-3.5 py-3 leading-[1.55] text-danger">
						{{ summaryError }}
					</p>

					<p
						v-if="detailError && detail"
						class="rounded-[14px] border border-line bg-[rgba(255,255,255,0.75)] px-3.5 py-3 leading-[1.55] text-ink-soft"
					>
						Unable to refresh details: {{ detailError }}
					</p>

					<div v-else-if="detailError && detail === null" class="grid gap-3 rounded-[16px] border border-line bg-white/60 p-4">
						<p class="leading-[1.6] text-danger">{{ detailError }}</p>
						<div>
							<button
								class="inline-flex min-h-11 items-center justify-center rounded-full border border-line bg-white px-4 font-bold text-ink-strong transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_20px_rgba(24,34,47,0.08)]"
								type="button"
								@click.stop="loadDetail(false)"
							>
								Retry detail load
							</button>
						</div>
					</div>

					<div v-else-if="detail" class="grid xl:grid-cols-3">
						<section
							data-anchor="request-log-received-request"
							class="flex flex-col h-180 gap-3 overflow-y-auto border-r border-line bg-surface p-4"
						>
							<div>
								<h3 class="mb-1 text-xs font-bold uppercase tracking-widest text-accent">Received request</h3>
							</div>

							<code class="wrap-break-word rounded-[16px] border border-line bg-white/70 px-3.5 py-3 text-[0.84rem] text-ink"
								>{{ detail.receivedRequest.method }} {{ formatPath(detail.receivedRequest.path, detail.receivedRequest.rawQuery) }}</code
							>

							<div class="grid gap-2">
								<span class="text-sm font-bold text-ink-strong">Headers</span>
								<pre
									class="m-0 overflow-auto rounded-[16px] border border-line bg-white/70 p-3.5 text-[0.84rem] leading-[1.65] text-ink"
								><code>{{ formatHeaders(detail.receivedRequest.headers) }}</code></pre>
							</div>

							<div class="grid gap-2">
								<span class="text-sm font-bold text-ink-strong">Body</span>
								<pre
									class="m-0 overflow-auto rounded-[16px] border border-line bg-white/70 p-3.5 text-[0.84rem] leading-[1.65] text-ink"
								><code>{{ formatBody(detail.receivedRequest.body, detail.receivedRequest.bodyTruncated, detail.receivedRequest.headers) }}</code></pre>
							</div>
						</section>

						<section
							data-anchor="request-log-sent-request"
							class="flex flex-col h-180 gap-3 overflow-y-auto border-r border-line bg-surface p-4"
						>
							<div>
								<h3 class="mb-1 text-xs font-bold uppercase tracking-widest text-accent">Sent request</h3>
							</div>

							<template v-if="detail.sentRequest">
								<code class="wrap-break-word rounded-[16px] border border-line bg-white/70 px-3.5 py-3 text-[0.84rem] text-ink"
									>{{ detail.sentRequest.method }} {{ detail.sentRequest.url }}</code
								>

								<div class="grid gap-2">
									<span class="text-sm font-bold text-ink-strong">Headers</span>
									<pre
										class="m-0 overflow-auto rounded-[16px] border border-line bg-white/70 p-3.5 text-[0.84rem] leading-[1.65] text-ink"
									><code>{{ formatHeaders(detail.sentRequest.headers) }}</code></pre>
								</div>

								<div class="grid gap-2">
									<span class="text-sm font-bold text-ink-strong">Body</span>
									<pre
										class="m-0 overflow-auto rounded-[16px] border border-line bg-white/70 p-3.5 text-[0.84rem] leading-[1.65] text-ink"
									><code>{{ formatBody(detail.sentRequest.body, detail.sentRequest.bodyTruncated, detail.sentRequest.headers) }}</code></pre>
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
								><code>{{ formatHeaders(detail.receivedResponse.headers) }}</code></pre>
							</div>

							<div class="grid gap-2">
								<span class="text-sm font-bold text-ink-strong">Body</span>
								<pre
									class="m-0 overflow-auto rounded-[16px] border border-line bg-white/70 p-3.5 text-[0.84rem] leading-[1.65] text-ink"
								><code>{{ formatBody(detail.receivedResponse.body, detail.receivedResponse.bodyTruncated, detail.receivedResponse.headers) }}</code></pre>
							</div>
						</section>
					</div>
				</div>
			</div>
		</Transition>
	</article>
</template>
