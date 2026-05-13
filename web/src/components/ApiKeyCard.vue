<script setup lang="ts">
import type { GatewayApiKeyView } from '../types'
import { Key, Clock, ClockCheck } from '@lucide/vue'
import { computed } from 'vue'

const props = defineProps<{
	apiKey: GatewayApiKeyView
	deleting: boolean
}>()

const emit = defineEmits<{
	(e: 'delete'): void
}>()

const dateFormatter = new Intl.DateTimeFormat(undefined, {
	dateStyle: 'medium',
	timeStyle: 'short',
})

const usageClass = computed(() =>
	props.apiKey.lastUsedAt ? 'border border-accent-soft bg-accent-soft text-accent' : 'border border-line bg-white/70 text-ink-soft',
)

// formatTimestamp converts an RFC3339 timestamp into a locale-friendly string.
function formatTimestamp(value: string) {
	const parsed = new Date(value)
	if (Number.isNaN(parsed.valueOf())) {
		return value
	}

	return dateFormatter.format(parsed)
}
</script>

<template>
	<article data-anchor="api-key-card" class="grid gap-2 rounded-card border border-line bg-surface-strong px-4.5 py-2">
		<div class="flex items-center justify-between gap-3">
			<div class="flex items-baseline gap-2">
				<h3>{{ props.apiKey.name }}</h3>
				<code
					class="mt-1.5 text-[0.76rem] leading-[1.6] text-ink-soft inline-flex items-center gap-1 bg-white/70 border rounded-full px-2.5 py-1 border-line"
				>
					<Key class="size-3" /> {{ props.apiKey.keyPrefix }}****
				</code>
			</div>
			<span
				:class="['inline-flex items-center rounded-full px-2.5 py-1 font-mono text-[0.76rem] font-bold uppercase tracking-widest', usageClass]"
			>
				{{ props.apiKey.lastUsedAt ? 'Used' : 'Unused' }}
			</span>
		</div>

		<div class="grid grid-cols-[1fr_auto]">
			<div class="flex flex-col gap-y-1 text-sm text-ink-soft col-span-1 font-mono">
				<div class="flex gap-2 items-center">
					<Clock class="size-3.5" />
					<span class="min-w-22">Created at</span>
					<span>{{ formatTimestamp(props.apiKey.createdAt) }}</span>
				</div>
				<div class="flex gap-2 items-center">
					<ClockCheck class="size-3.5" />
					<span class="min-w-22">Last used at</span>
					<span v-if="props.apiKey.lastUsedAt">{{ formatTimestamp(props.apiKey.lastUsedAt) }}</span>
					<span v-else>-</span>
				</div>
			</div>

			<div class="flex flex-wrap items-center justify-start gap-3 max-lg:flex-col max-lg:items-stretch col-span-1">
				<button class="btn-danger min-h-8 text-sm" type="button" :disabled="props.deleting" @click="emit('delete')">
					{{ props.deleting ? 'Revoking…' : 'Revoke' }}
				</button>
			</div>
		</div>
	</article>
</template>
