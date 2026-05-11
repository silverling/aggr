<script setup lang="ts">
import type { ProviderView } from '../types'

const props = defineProps<{
	provider: ProviderView
	syncing: boolean
}>()

const emit = defineEmits<{
	(e: 'edit'): void
	(e: 'sync'): void
	(e: 'delete'): void
}>()

const dateFormatter = new Intl.DateTimeFormat(undefined, {
	dateStyle: 'medium',
	timeStyle: 'short',
})

function formatTimestamp(value?: string) {
	if (!value) {
		return 'Not synced yet'
	}

	const parsed = new Date(value)
	if (Number.isNaN(parsed.valueOf())) {
		return value
	}

	return dateFormatter.format(parsed)
}
</script>

<template>
	<article data-anchor="provider-card" class="grid gap-3.5 rounded-card border border-line bg-surface-strong p-4.5">
		<div class="flex items-center justify-between gap-3 max-lg:items-start">
			<div>
				<h3>{{ props.provider.name }}</h3>
				<p class="mt-1.5 wrap-break-word text-sm text-ink-soft">{{ props.provider.baseUrl }}</p>
			</div>
			<span
				:class="[
					'inline-flex items-center rounded-full px-2.5 py-1 font-mono text-[0.76rem] font-bold uppercase tracking-widest',
					props.provider.enabled
						? 'border border-accent-soft bg-accent-soft text-accent'
						: 'border border-transparent bg-[rgba(24,34,47,0.08)] text-ink-soft',
				]"
			>
				{{ props.provider.enabled ? 'Enabled' : 'Disabled' }}
			</span>
		</div>

		<div class="flex flex-wrap items-center justify-between gap-3 text-sm text-ink-soft max-lg:items-start">
			<span>{{ props.provider.apiKeyConfigured ? props.provider.apiKeyPreview : 'No API key' }}</span>
			<span>{{ formatTimestamp(props.provider.lastSyncedAt) }}</span>
		</div>

		<p v-if="props.provider.lastError" class="m-0 rounded-[14px] bg-danger-soft px-3.5 py-3 leading-normal text-danger">
			{{ props.provider.lastError }}
		</p>

		<div v-if="props.provider.models.length > 0" class="flex flex-wrap gap-2.5">
			<span
				v-for="model in props.provider.models"
				:key="model"
				class="inline-flex items-center rounded-full border border-accent-soft bg-[rgba(12,118,98,0.08)] px-3 py-2 font-mono text-[0.82rem] font-bold text-accent"
			>
				{{ model }}
			</span>
		</div>
		<p v-else class="text-ink-soft">No models synced yet.</p>

		<div class="flex flex-wrap items-center justify-start gap-3 max-lg:flex-col max-lg:items-stretch">
			<button
				class="inline-flex min-h-12 items-center justify-center rounded-full border border-line bg-[rgba(255,255,255,0.72)] px-4.5 font-bold text-ink-strong transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
				type="button"
				@click="emit('edit')"
			>
				Edit
			</button>
			<button
				class="inline-flex min-h-12 items-center justify-center rounded-full border border-line bg-[rgba(255,255,255,0.72)] px-4.5 font-bold text-ink-strong transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
				type="button"
				:disabled="props.syncing"
				@click="emit('sync')"
			>
				{{ props.syncing ? 'Syncing…' : 'Sync models' }}
			</button>
			<button
				class="inline-flex min-h-12 items-center justify-center rounded-full border border-[rgba(164,63,63,0.2)] bg-[rgba(255,255,255,0.72)] px-4.5 font-bold text-danger transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
				type="button"
				@click="emit('delete')"
			>
				Delete
			</button>
		</div>
	</article>
</template>
