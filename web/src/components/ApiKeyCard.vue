<script setup lang="ts">
import type { GatewayApiKeyView } from '../types'
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
	<article data-anchor="api-key-card" class="grid gap-3.5 rounded-card border border-line bg-surface-strong p-4.5">
		<div class="flex items-start justify-between gap-3">
			<div>
				<h3>{{ props.apiKey.name }}</h3>
				<p class="mt-1.5 leading-[1.6] text-ink-soft">Prefix: {{ props.apiKey.keyPrefix }}</p>
			</div>
			<span
				:class="['inline-flex items-center rounded-full px-2.5 py-1 font-mono text-[0.76rem] font-bold uppercase tracking-widest', usageClass]"
			>
				{{ props.apiKey.lastUsedAt ? 'Used' : 'Unused' }}
			</span>
		</div>

		<div class="flex flex-wrap gap-x-3 gap-y-1.5 text-sm text-ink-soft">
			<span>Created {{ formatTimestamp(props.apiKey.createdAt) }}</span>
			<span v-if="props.apiKey.lastUsedAt">Last used {{ formatTimestamp(props.apiKey.lastUsedAt) }}</span>
		</div>

		<div class="flex flex-wrap items-center justify-start gap-3 max-lg:flex-col max-lg:items-stretch">
			<button class="btn-danger" type="button" :disabled="props.deleting" @click="emit('delete')">
				{{ props.deleting ? 'Revoking…' : 'Revoke API key' }}
			</button>
		</div>
	</article>
</template>
