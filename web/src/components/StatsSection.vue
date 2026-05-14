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
	<section data-anchor="request-stats" class="rounded-panel border border-line bg-surface p-5 shadow-panel backdrop-blur-[18px] lg:p-7">
		<div class="mb-5 gap-3 max-lg:flex-col max-lg:items-stretch">
			<div>
				<p class="mb-3 eyebrow">Stats</p>
				<div class="flex w-full">
					<h2>Traffic and usage</h2>
					<label class="flex items-center gap-2 ml-auto">
						<select data-anchor="stats-range" :value="props.range" class="select min-w-50" @change="onRangeChange">
							<option v-for="option in props.rangeOptions" :key="option.value" :value="option.value">
								{{ option.label }}
							</option>
						</select>
					</label>
				</div>
			</div>
		</div>

		<p v-if="props.error" class="mb-5 rounded-[14px] bg-danger-soft px-3.5 py-3 leading-normal text-danger">
			{{ props.error }}
		</p>

		<div v-if="props.stats" class="grid gap-4.5">
			<div class="grid gap-4.5 md:grid-cols-2 xl:grid-cols-4">
				<StatCard label="Requests" :value="props.stats.summary.requests" description="Audited requests in the selected range" />
				<StatCard label="Succeeded" :value="props.stats.summary.succeeded" description="Completed requests with 2xx responses" />
				<StatCard label="Failed" :value="props.stats.summary.failed" description="Completed requests with non-2xx responses" />
				<StatCard label="Ongoing" :value="props.stats.summary.ongoingRequests" description="Requests whose responses have not finished yet" />
				<StatCard label="Consumed Tokens" :value="props.stats.summary.consumedTokens" description="Input and output tokens" />
				<StatCard label="Cached Input" :value="props.stats.summary.cachedInputTokens" description="Input tokens served from cache" />
				<StatCard label="Non-Cached Input" :value="props.stats.summary.nonCachedInputTokens" description="Input tokens that were not cached" />
				<StatCard label="Output Tokens" :value="props.stats.summary.outputTokens" description="Generated output tokens" />
			</div>

			<div class="grid gap-4.5 xl:grid-cols-2">
				<TokenUsageChart title="Recent 7 days" :buckets="props.stats.daily" bucket-label-kind="day" />
				<TokenUsageChart title="Recent 12 hours" :buckets="props.stats.hourly" bucket-label-kind="hour" />
			</div>
		</div>

		<div
			v-else-if="props.loading"
			class="rounded-card border border-dashed border-line bg-surface-strong px-5.5 py-6.5 leading-[1.6] text-ink-soft"
		>
			Loading stats…
		</div>

		<div v-else class="rounded-card border border-line bg-surface-strong px-5.5 py-6.5 leading-[1.6] text-ink-soft">
			No stats are available yet.
		</div>
	</section>
</template>
