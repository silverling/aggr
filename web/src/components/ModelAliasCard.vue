<script setup lang="ts">
import type { ModelAliasView } from '../types'

const props = defineProps<{
	alias: ModelAliasView
	editing: boolean
}>()

const emit = defineEmits<{
	(e: 'edit'): void
	(e: 'delete'): void
}>()

const dateFormatter = new Intl.DateTimeFormat(undefined, {
	dateStyle: 'medium',
	timeStyle: 'short',
})

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
		data-anchor="model-alias-card"
		:class="[
			'grid gap-3.5 rounded-card border bg-surface-strong p-4.5',
			props.editing ? 'border-[rgba(12,118,98,0.3)] shadow-[0_14px_34px_rgba(12,118,98,0.08)]' : 'border-line',
		]"
	>
		<div class="flex items-start justify-between gap-3">
			<div>
				<h3>{{ props.alias.aliasModelId }}</h3>
				<p class="mt-1.5 leading-[1.6] text-ink-soft">
					Routes to {{ props.alias.targetModelId }}
					<template v-if="props.alias.targetProviderName"> on {{ props.alias.targetProviderName }}</template>
					<template v-else> on any enabled provider</template>
				</p>
			</div>
			<span
				:class="[
					'inline-flex items-center rounded-full px-2.5 py-1 font-mono text-[0.76rem] font-bold uppercase tracking-widest',
					props.alias.routable
						? 'border border-accent-soft bg-accent-soft text-accent'
						: 'border border-[rgba(164,63,63,0.18)] bg-danger-soft text-danger',
				]"
			>
				{{ props.alias.routable ? 'Routable' : 'Unavailable' }}
			</span>
		</div>

		<div class="flex flex-wrap items-center gap-x-3 gap-y-1.5 text-sm text-ink-soft">
			<span>Updated {{ formatTimestamp(props.alias.updatedAt) }}</span>
			<span v-if="props.alias.targetProviderName">Pinned to {{ props.alias.targetProviderName }}</span>
		</div>

		<div v-if="props.alias.providers.length > 0" class="flex flex-wrap gap-2.5">
			<span
				v-for="provider in props.alias.providers"
				:key="provider.id"
				class="inline-flex items-center rounded-full border border-accent-soft bg-[rgba(12,118,98,0.08)] px-3 py-2 font-mono text-[0.82rem] font-bold text-accent"
			>
				{{ provider.name }}
			</span>
		</div>
		<p v-else class="rounded-[16px] border border-dashed border-line bg-white/50 px-3.5 py-4 leading-[1.6] text-ink-soft">
			No enabled provider currently routes this alias.
		</p>

		<div class="flex flex-wrap items-center justify-start gap-3 max-lg:flex-col max-lg:items-stretch">
			<button
				class="inline-flex min-h-12 items-center justify-center rounded-full border border-line bg-[rgba(255,255,255,0.72)] px-4.5 font-bold text-ink-strong transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60 max-lg:w-full"
				type="button"
				@click="emit('edit')"
			>
				Edit
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
