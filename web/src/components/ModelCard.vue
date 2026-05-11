<script setup lang="ts">
import type { ModelDisableRuleSelection, ModelRoute, ModelProviderSummary } from '../types'

const props = defineProps<{
	model: ModelRoute
	selectedRule: ModelDisableRuleSelection | null
}>()

const emit = defineEmits<{
	(e: 'select-rule', provider: ModelProviderSummary): void
}>()

function isSelected(providerId: number) {
	return props.selectedRule?.providerId === providerId && props.selectedRule.modelId === props.model.id
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
					isSelected(provider.id)
						? 'border-[rgba(24,34,47,0.24)] bg-[rgba(24,34,47,0.12)] text-ink-strong shadow-[0_10px_24px_rgba(24,34,47,0.08)]'
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
