<script setup lang="ts">
import { computed } from 'vue'

import type { RequestStatsBucket } from '../types'

const props = defineProps<{
	title: string
	subtitle: string
	buckets: RequestStatsBucket[]
}>()

const compactNumberFormatter = new Intl.NumberFormat(undefined, {
	maximumFractionDigits: 0,
	notation: 'compact',
})

const maxConsumedTokens = computed(() =>
	Math.max(
		1,
		...props.buckets.map((bucket) => bucket.cachedInputTokens + bucket.nonCachedInputTokens + bucket.outputTokens),
	),
)

function formatCompactNumber(value: number) {
	return compactNumberFormatter.format(value)
}

function segmentHeight(tokens: number) {
	return `${Math.max(0, (tokens / maxConsumedTokens.value) * 100)}%`
}

function bucketTitle(bucket: RequestStatsBucket) {
	return [
		`${bucket.label}`,
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
		<div class="flex items-start justify-between gap-3">
			<div>
				<h3>{{ props.title }}</h3>
				<p class="mt-1.5 leading-[1.6] text-ink-soft">{{ props.subtitle }}</p>
			</div>
			<span class="rounded-full border border-line bg-white/70 px-3 py-1.5 font-mono text-[0.8rem] font-bold text-ink-strong">
				Max {{ formatCompactNumber(maxConsumedTokens) }}
			</span>
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
			class="grid min-h-[260px] items-end gap-3"
			:style="{ gridTemplateColumns: `repeat(${props.buckets.length}, minmax(0, 1fr))` }"
		>
			<div v-for="bucket in props.buckets" :key="bucket.start" class="grid gap-2">
				<span class="text-center font-mono text-[0.78rem] text-ink-soft">{{ bucket.consumedTokens === 0 ? '0' : formatCompactNumber(bucket.consumedTokens) }}</span>
				<div
					class="relative flex h-[190px] items-end justify-center rounded-[18px] border border-line bg-[rgba(255,255,255,0.52)] px-2 pb-2 pt-4"
					:title="bucketTitle(bucket)"
				>
					<div class="flex h-full w-full max-w-[42px] flex-col justify-end overflow-hidden rounded-[14px] bg-[rgba(24,34,47,0.05)]">
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
				</div>
				<div class="grid gap-1 text-center">
					<span class="font-mono text-[0.8rem] font-bold text-ink-strong">{{ bucket.label }}</span>
					<span class="text-[0.76rem] text-ink-soft">{{ bucket.requests }} req</span>
				</div>
			</div>
		</div>
	</article>
</template>
