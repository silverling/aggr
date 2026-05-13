<script setup lang="ts">
import { formatCompactNumber } from '../lib/utils'
import type { RequestStatsBucket } from '../types'
import { computed } from 'vue'

const props = defineProps<{
	title: string
	subtitle?: string
	buckets: RequestStatsBucket[]
	bucketLabelKind: 'day' | 'hour'
}>()

const dailyLabelFormatter = new Intl.DateTimeFormat(undefined, {
	month: 'short',
	day: 'numeric',
})

const hourlyLabelFormatter = new Intl.DateTimeFormat(undefined, {
	hour: 'numeric',
	minute: '2-digit',
	hour12: false,
})

const tooltipFormatter = new Intl.DateTimeFormat(undefined, {
	dateStyle: 'medium',
	timeStyle: 'short',
})

const maxConsumedTokens = computed(() =>
	Math.max(1, ...props.buckets.map((bucket) => bucket.cachedInputTokens + bucket.nonCachedInputTokens + bucket.outputTokens)),
)

// bucketStart parses the RFC3339 bucket start timestamp so labels and tooltips
// can be rendered in the viewer's local timezone.
function bucketStart(bucket: RequestStatsBucket) {
	const parsed = new Date(bucket.start)
	if (Number.isNaN(parsed.valueOf())) {
		return null
	}

	return parsed
}

function segmentHeight(tokens: number) {
	return `${Math.max(0, (tokens / maxConsumedTokens.value) * 100)}%`
}

// bucketLabel derives a user-local chart label from the bucket start time while
// preserving the API label as a fallback for malformed timestamps.
function bucketLabel(bucket: RequestStatsBucket) {
	const parsed = bucketStart(bucket)
	if (parsed === null) {
		return bucket.label
	}

	return props.bucketLabelKind === 'day' ? dailyLabelFormatter.format(parsed) : hourlyLabelFormatter.format(parsed)
}

// bucketTimeRange describes the bucket's start in the user's local timezone so
// hover tooltips align with the displayed chart labels.
function bucketTimeRange(bucket: RequestStatsBucket) {
	const parsed = bucketStart(bucket)
	if (parsed === null) {
		return bucket.label
	}

	return tooltipFormatter.format(parsed)
}

function bucketTitle(bucket: RequestStatsBucket) {
	return [
		`${bucketTimeRange(bucket)}`,
		`Requests: ${bucket.requests}`,
		`Succeeded: ${bucket.succeeded}`,
		`Failed: ${bucket.failed}`,
		`Consumed: ${bucket.consumedTokens}`,
		`Cached input: ${bucket.cachedInputTokens}`,
		`Non-cached input: ${bucket.nonCachedInputTokens}`,
		`Output: ${bucket.outputTokens}`,
	].join('\n')
}
</script>

<template>
	<article data-anchor="token-usage-chart" class="grid gap-4 rounded-card border border-line bg-surface-strong p-4.5">
		<div class="grid gap-1">
			<h3 class="flex justify-between items-center">
				<span>{{ props.title }}</span>
				<span class="ml-auto rounded-full border border-line bg-white/70 px-3 py-1.5 font-mono text-[0.8rem] font-bold text-ink-strong">
					Max {{ formatCompactNumber(maxConsumedTokens) }}
				</span>
			</h3>
			<p v-if="props.subtitle" class="mt-1.5 leading-[1.6] text-ink-soft">{{ props.subtitle }}</p>
		</div>

		<div class="flex flex-wrap items-center gap-3 text-sm text-ink-soft">
			<span class="inline-flex items-center gap-2">
				<span class="h-2.5 w-2.5 rounded-full bg-accent"></span>
				Non-cached input
			</span>
			<span class="inline-flex items-center gap-2">
				<span class="h-2.5 w-2.5 rounded-full bg-[rgba(12,118,98,0.32)]"></span>
				Cached input
			</span>
			<span class="inline-flex items-center gap-2">
				<span class="h-2.5 w-2.5 rounded-full bg-[rgba(200,93,53,0.86)]"></span>
				Output
			</span>
		</div>

		<div
			v-if="props.buckets.length > 0"
			class="grid min-h-65 items-end gap-3"
			:style="{ gridTemplateColumns: `repeat(${props.buckets.length}, minmax(0, 1fr))` }"
		>
			<div v-for="bucket in props.buckets" :key="bucket.start" class="grid gap-2">
				<span class="text-center font-mono text-[0.78rem] text-ink-soft">{{
					bucket.consumedTokens === 0 ? '0' : formatCompactNumber(bucket.consumedTokens)
				}}</span>

				<div
					class="mx-auto flex h-47.5 w-full max-w-12 flex-col justify-end overflow-hidden rounded-xs bg-[rgba(24,34,47,0.05)]"
					:title="bucketTitle(bucket)"
				>
					<div
						v-if="bucket.outputTokens > 0"
						class="w-full bg-[rgba(200,93,53,0.86)]"
						:style="{ height: segmentHeight(bucket.outputTokens) }"
					></div>
					<div
						v-if="bucket.cachedInputTokens > 0"
						class="w-full bg-[rgba(12,118,98,0.32)]"
						:style="{ height: segmentHeight(bucket.cachedInputTokens) }"
					></div>
					<div
						v-if="bucket.nonCachedInputTokens > 0"
						class="w-full bg-accent"
						:style="{ height: segmentHeight(bucket.nonCachedInputTokens) }"
					></div>
				</div>
				<div class="grid gap-1 text-center">
					<span class="font-mono text-[0.8rem] font-bold text-ink-strong">{{ bucketLabel(bucket) }}</span>
					<span class="text-[9px] text-ink-soft">{{ formatCompactNumber(bucket.requests) }} Req</span>
				</div>
			</div>
		</div>
	</article>
</template>
