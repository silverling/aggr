<script setup lang="ts">
import { computed } from 'vue'

import type { AuthSessionView } from '../types'

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
	props.session.current
		? 'border border-accent-soft bg-accent-soft text-accent'
		: 'border border-line bg-white/70 text-ink-soft',
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
	<article
		data-anchor="session-card"
		class="grid gap-3.5 rounded-card border border-line bg-surface-strong p-4.5"
	>
		<div class="flex items-start justify-between gap-3">
			<div>
				<h3>Session #{{ props.session.id }}</h3>
				<p class="mt-1.5 leading-[1.6] text-ink-soft">Remote: {{ props.session.remoteAddr ?? 'Unknown' }}</p>
			</div>
			<span
				:class="[
					'inline-flex items-center rounded-full px-2.5 py-1 font-mono text-[0.76rem] font-bold uppercase tracking-widest',
					currentBadgeClass,
				]"
			>
				{{ props.session.current ? 'Current' : 'Saved' }}
			</span>
		</div>

		<p class="wrap-break-word text-sm text-ink-soft">
			User agent: {{ props.session.userAgent ?? 'Unknown' }}
		</p>

		<div class="flex flex-wrap gap-x-3 gap-y-1.5 text-sm text-ink-soft">
			<span>Created {{ formatTimestamp(props.session.createdAt) }}</span>
			<span>Last seen {{ formatTimestamp(props.session.lastSeenAt) }}</span>
		</div>

		<div class="flex flex-wrap items-center justify-start gap-3 max-lg:flex-col max-lg:items-stretch">
			<button
				class="inline-flex min-h-12 items-center justify-center rounded-full border border-[rgba(164,63,63,0.2)] bg-[rgba(255,255,255,0.72)] px-4.5 font-bold text-danger transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
				type="button"
				:disabled="props.deleting"
				@click="emit('delete')"
			>
				{{ props.deleting ? 'Revoking…' : 'Revoke session' }}
			</button>
		</div>
	</article>
</template>
