<script setup lang="ts">
import type { PendingModelDisableRule, ProviderView } from '../types'
import { Key } from '@lucide/vue'

const props = defineProps<{
	provider: ProviderView
	syncing: boolean
	pendingRules: PendingModelDisableRule[]
}>()

const emit = defineEmits<{
	(e: 'edit'): void
	(e: 'sync'): void
	(e: 'delete'): void
	(e: 'select-rule', modelId: string): void
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

function isModelDisabled(modelId: string) {
	return props.provider.disabledModels.includes(modelId)
}

function isSelected(modelId: string) {
	return props.pendingRules.some((rule) => rule.providerId === props.provider.id && rule.modelId === modelId)
}

function pendingRule(modelId: string) {
	return props.pendingRules.find((rule) => rule.providerId === props.provider.id && rule.modelId === modelId) ?? null
}
</script>

<template>
	<article data-anchor="provider-card" class="grid gap-3.5 rounded-card border border-line bg-surface-strong p-4.5">
		<div class="flex items-center gap-3">
			<h3>{{ props.provider.name }}</h3>
			<span class="badge wrap-break-word font-thin">{{ props.provider.baseUrl }}</span>
			<span class="badge font-thin">
				<Key class="size-2.5" />
				{{ props.provider.apiKeyConfigured ? props.provider.apiKeyPreview : 'N/A' }}
			</span>
			<span
				:class="[
					'badge uppercase ml-auto',
					props.provider.enabled
						? 'border border-accent-soft bg-accent-soft text-accent'
						: 'border border-transparent bg-[rgba(24,34,47,0.08)] text-ink-soft',
				]"
			>
				{{ props.provider.enabled ? 'Enabled' : 'Disabled' }}
			</span>
		</div>

		<div class="flex flex-wrap items-center justify-between gap-3 text-sm text-ink-soft max-lg:items-start">
			<span class="wrap-break-word space-x-2">
				<span>User agent:</span>
				<span class="text-ink-strong">{{ props.provider.userAgent ?? 'Default' }}</span>
			</span>
			<span class="space-x-1">
				<span>Last sync at</span>
				<span class="text-ink-strong">{{ formatTimestamp(props.provider.lastSyncedAt) }}</span>
			</span>
		</div>

		<p v-if="props.provider.lastError" class="m-0 rounded-[14px] bg-danger-soft px-3.5 py-3 leading-normal text-danger">
			{{ props.provider.lastError }}
		</p>

		<div v-if="props.provider.models.length > 0" class="flex flex-wrap gap-2.5">
			<button
				v-for="model in props.provider.models"
				:key="model"
				data-anchor="provider-card-model"
				:class="[
					'badge badge-lg transition duration-150 ease-out hover:-translate-y-px',
					pendingRule(model)?.disabled
						? 'border-[rgba(164,63,63,0.24)] bg-danger-soft text-danger shadow-[0_10px_24px_rgba(24,34,47,0.08)]'
						: isSelected(model)
							? 'border-[rgba(12,118,98,0.24)] bg-[rgba(12,118,98,0.12)] text-accent shadow-[0_10px_24px_rgba(24,34,47,0.08)]'
							: isModelDisabled(model)
								? 'border-[rgba(164,63,63,0.18)] bg-danger-soft text-danger'
								: 'border-accent-soft bg-[rgba(12,118,98,0.08)] text-accent',
				]"
				type="button"
				@click="emit('select-rule', model)"
			>
				{{ model }}
			</button>
		</div>
		<p v-else class="text-ink-soft">No models synced yet.</p>

		<div class="flex flex-wrap items-center justify-start gap-3 max-lg:flex-col max-lg:items-stretch">
			<button class="btn" type="button" @click="emit('edit')">Edit</button>
			<button class="btn" type="button" :disabled="props.syncing" @click="emit('sync')">
				{{ props.syncing ? 'Syncing…' : 'Sync models' }}
			</button>
			<button class="btn btn-danger" type="button" @click="emit('delete')">Delete</button>
		</div>
	</article>
</template>
