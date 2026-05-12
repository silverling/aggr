<script setup lang="ts">
import type { ModelRoute, ModelProviderSummary, PendingModelDisableRule } from '../types'

const props = defineProps<{
	model: ModelRoute
	pendingRules: PendingModelDisableRule[]
}>()

const emit = defineEmits<{
	(e: 'select-rule', provider: ModelProviderSummary): void
}>()

function isSelected(providerId: number) {
	return props.pendingRules.some((rule) => rule.providerId === providerId && rule.modelId === props.model.id)
}

function pendingRule(providerId: number) {
	return props.pendingRules.find((rule) => rule.providerId === providerId && rule.modelId === props.model.id) ?? null
}
</script>

<template>
	<article data-anchor="model-card" class="grid gap-3.5 rounded-card border border-line bg-surface-strong p-4.5">
		<h3 class="font-mono text-[0.98rem] wrap-break-word">{{ props.model.id }}</h3>
		<div class="flex flex-wrap gap-2">
			<button
				v-for="provider in props.model.providers"
				:key="provider.id"
				data-anchor="model-card-provider"
				:class="[
					'inline-flex items-center rounded-full border px-3 py-2 font-mono text-[0.82rem] font-bold transition duration-150 ease-out hover:-translate-y-px',
					pendingRule(provider.id)?.disabled
						? 'border-[rgba(164,63,63,0.24)] bg-[rgba(164,63,63,0.12)] text-danger shadow-[0_10px_24px_rgba(24,34,47,0.08)]'
						: isSelected(provider.id)
							? 'border-[rgba(12,118,98,0.24)] bg-[rgba(12,118,98,0.12)] text-accent shadow-[0_10px_24px_rgba(24,34,47,0.08)]'
						: 'border-[rgba(200,93,53,0.14)] bg-[rgba(200,93,53,0.08)] text-accent-strong',
				]"
				type="button"
				@click="emit('select-rule', provider)"
			>
				{{ provider.name }}
			</button>
		</div>
	</article>
</template>
