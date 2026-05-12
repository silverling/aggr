<script setup lang="ts">
import type { RequestStatsView, StatsRangeOption } from '../types'
import StatCard from './StatCard.vue'
import TokenUsageChart from './TokenUsageChart.vue'

const props = defineProps<{
	stats: RequestStatsView | null
	range: string
	rangeOptions: StatsRangeOption[]
	loading: boolean
	error: string
}>()

const emit = defineEmits<{
	(e: 'update:range', value: string): void
}>()

function onRangeChange(event: Event) {
	const target = event.target as HTMLSelectElement
	emit('update:range', target.value)
}
</script>

<template>
	<section
		data-anchor="request-stats"
		class="rounded-[var(--radius-panel)] border border-line bg-surface p-5 shadow-panel backdrop-blur-[18px] lg:p-7"
	>
		<div class="mb-5 flex items-start justify-between gap-3 max-lg:flex-col max-lg:items-stretch">
			<div>
				<p class="mb-3 text-xs font-bold uppercase tracking-[0.1em] text-accent">Stats</p>
				<h2>Gateway traffic and token usage</h2>
				<p v-if="props.stats" class="mt-1.5 leading-[1.6] text-ink-soft">Summary window: {{ props.stats.rangeLabel }}</p>
			</div>

			<div class="flex flex-wrap items-center gap-3 max-lg:flex-col max-lg:items-stretch">
				<label class="grid gap-2">
					<span class="text-[0.92rem] font-bold text-ink-strong">Date range</span>
					<select
						data-anchor="stats-range"
						:value="props.range"
						class="min-w-[200px] rounded-[var(--radius-field)] border border-line-strong bg-white/90 px-4 py-[15px] text-ink-strong outline-none transition duration-150 ease-out focus:-translate-y-px focus:border-[rgba(12,118,98,0.45)] focus:shadow-[0_0_0_4px_rgba(12,118,98,0.1)]"
						@change="onRangeChange"
					>
						<option v-for="option in props.rangeOptions" :key="option.value" :value="option.value">
							{{ option.label }}
						</option>
					</select>
				</label>
				<span
					v-if="props.loading"
					class="inline-flex min-h-12 items-center rounded-full border border-line bg-white/70 px-4 font-mono text-[0.8rem] font-bold text-ink-soft"
				>
					Refreshing…
				</span>
			</div>
		</div>

		<p v-if="props.error" class="mb-5 rounded-[14px] bg-danger-soft px-3.5 py-3 leading-normal text-danger">
			{{ props.error }}
		</p>

		<div v-if="props.stats" class="grid gap-[18px]">
			<div class="grid gap-[18px] md:grid-cols-2 xl:grid-cols-4">
				<StatCard label="Requests" :value="props.stats.summary.requests" description="Audited requests in the selected range" />
				<StatCard label="Succeeded" :value="props.stats.summary.succeeded" description="Completed requests with 2xx responses" />
				<StatCard label="Failed" :value="props.stats.summary.failed" description="Completed requests with non-2xx responses" />
				<StatCard label="Ongoing" :value="props.stats.summary.ongoingRequests" description="Requests whose responses have not finished yet" />
				<StatCard label="Consumed Tokens" :value="props.stats.summary.consumedTokens" description="Input plus output tokens" />
				<StatCard label="Cached Input" :value="props.stats.summary.cachedInputTokens" description="Input tokens served from cache" />
				<StatCard label="Non-Cached Input" :value="props.stats.summary.nonCachedInputTokens" description="Input tokens that were not cached" />
				<StatCard label="Output Tokens" :value="props.stats.summary.outputTokens" description="Generated output tokens" />
			</div>

			<div class="grid gap-[18px] xl:grid-cols-2">
				<TokenUsageChart
					title="Recent 7 days"
					subtitle="Token usage grouped by day from the last seven calendar days."
					:buckets="props.stats.daily"
				/>
				<TokenUsageChart
					title="Recent 12 hours"
					subtitle="Token usage grouped by hour from the last twelve clock hours."
					:buckets="props.stats.hourly"
				/>
			</div>
		</div>

		<div
			v-else-if="props.loading"
			class="rounded-[var(--radius-card)] border border-dashed border-line bg-surface-strong px-[22px] py-[26px] leading-[1.6] text-ink-soft"
		>
			Loading stats…
		</div>

		<div v-else class="rounded-[var(--radius-card)] border border-line bg-surface-strong px-[22px] py-[26px] leading-[1.6] text-ink-soft">
			No stats are available yet.
		</div>
	</section>
</template>
