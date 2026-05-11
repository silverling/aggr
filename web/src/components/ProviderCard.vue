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
	<article class="provider-card">
		<div class="provider-topline">
			<div>
				<h3>{{ props.provider.name }}</h3>
				<p class="provider-url">{{ props.provider.baseUrl }}</p>
			</div>
			<span :class="['badge', props.provider.enabled ? 'badge-enabled' : 'badge-disabled']">
				{{ props.provider.enabled ? 'Enabled' : 'Disabled' }}
			</span>
		</div>

		<div class="provider-meta">
			<span>{{ props.provider.apiKeyConfigured ? props.provider.apiKeyPreview : 'No API key' }}</span>
			<span>{{ formatTimestamp(props.provider.lastSyncedAt) }}</span>
		</div>

		<p v-if="props.provider.lastError" class="provider-error">
			{{ props.provider.lastError }}
		</p>

		<div v-if="props.provider.models.length > 0" class="chip-row">
			<span v-for="model in props.provider.models" :key="model" class="chip">
				{{ model }}
			</span>
		</div>
		<p v-else class="provider-hint">No models synced yet.</p>

		<div class="provider-actions">
			<button class="button button-subtle" type="button" @click="emit('edit')">Edit</button>
			<button class="button button-subtle" type="button" :disabled="props.syncing" @click="emit('sync')">
				{{ props.syncing ? 'Syncing…' : 'Sync models' }}
			</button>
			<button class="button button-danger" type="button" @click="emit('delete')">Delete</button>
		</div>
	</article>
</template>
