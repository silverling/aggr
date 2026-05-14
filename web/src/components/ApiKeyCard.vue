<script setup lang="ts">
import type { GatewayApiKeyView } from '../types'
import { Key } from '@lucide/vue'
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
	<article data-anchor="api-key-card" class="grid gap-4 rounded-card border border-line bg-surface-strong px-4.5 py-2">
		<div class="flex items-center justify-between gap-3">
			<div class="flex items-baseline gap-2">
				<h3>{{ props.apiKey.name }}</h3>
				<code class="badge text-ink-soft font-thin"><Key class="size-3" />{{ props.apiKey.keyPrefix }}****</code>
			</div>
			<span :class="['badge uppercase', usageClass]">
				{{ props.apiKey.lastUsedAt ? 'Used' : 'Unused' }}
			</span>
		</div>

		<div class="grid grid-cols-[1fr_auto]">
			<div class="flex flex-col gap-y-2 text-xs text-ink-soft col-span-1">
				<div class="flex gap-2 items-center">
					<span class="min-w-22">Created at</span>
					<span>{{ formatTimestamp(props.apiKey.createdAt) }}</span>
				</div>
				<div class="flex gap-2 items-center">
					<span class="min-w-22">Last used at</span>
					<span v-if="props.apiKey.lastUsedAt">{{ formatTimestamp(props.apiKey.lastUsedAt) }}</span>
					<span v-else>-</span>
				</div>
			</div>

			<div class="flex flex-wrap items-center justify-start gap-3 max-lg:flex-col max-lg:items-stretch col-span-1">
				<button class="btn btn-danger btn-sm" type="button" :disabled="props.deleting" @click="emit('delete')">
					{{ props.deleting ? 'Revoking…' : 'Revoke' }}
				</button>
			</div>
		</div>
	</article>
</template>
