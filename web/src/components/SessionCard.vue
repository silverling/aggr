<script setup lang="ts">
import type { AuthSessionView } from '../types'
import { computed } from 'vue'

const props = defineProps<{
	session: AuthSessionView
	deleting: boolean
}>()

const emit = defineEmits<{
	(e: 'delete'): void
}>()

const dateFormatter = new Intl.DateTimeFormat(undefined, {
	dateStyle: 'medium',
	timeStyle: 'short',
})

const currentBadgeClass = computed(() =>
	props.session.current ? 'border border-accent-soft bg-accent-soft text-accent' : 'border border-line bg-white/70 text-ink-soft',
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
	<article data-anchor="session-card" class="flex flex-col gap-3.5 rounded-card border border-line bg-surface-strong p-4.5">
		<div class="flex items-start justify-between gap-3">
			<h3>Session #{{ props.session.id }}</h3>
			<span :class="['badge uppercase', currentBadgeClass]">
				{{ props.session.current ? 'Current' : 'Saved' }}
			</span>
		</div>

		<p class="text-ink-soft text-sm">
			<span>Remote: </span>
			<span class="text-ink-strong">{{ props.session.remoteAddr ?? 'Unknown' }}</span>
		</p>

		<p class="wrap-break-word text-sm text-ink-soft space-x-2">
			<span>User agent:</span>
			<span class="text-ink-strong">{{ props.session.userAgent ?? 'Unknown' }}</span>
		</p>

		<div class="grid grid-cols-2 gap-x-3 gap-y-1.5 text-sm text-ink-soft">
			<span class="space-x-2">
				<span>Created at</span>
				<span class="text-ink-strong">{{ formatTimestamp(props.session.createdAt) }}</span>
			</span>
			<span class="space-x-2">
				<span>Last seen at</span>
				<span class="text-ink-strong">{{ formatTimestamp(props.session.lastSeenAt) }}</span>
			</span>
		</div>

		<div class="flex flex-wrap items-center justify-start gap-3 max-lg:flex-col max-lg:items-stretch">
			<button class="btn btn-danger btn-sm" type="button" :disabled="props.deleting" @click="emit('delete')">
				{{ props.deleting ? 'Revoking…' : 'Revoke session' }}
			</button>
		</div>
	</article>
</template>
