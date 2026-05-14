<script setup lang="ts">
import type { ModelAliasView } from '../types'
import { ArrowRight } from '@lucide/vue'

const props = defineProps<{
	alias: ModelAliasView
	editing: boolean
}>()

const emit = defineEmits<{
	(e: 'edit'): void
	(e: 'delete'): void
}>()
</script>

<template>
	<details
		data-anchor="model-alias-card"
		:class="[
			'collapse collapse-arrow flex flex-col rounded-card border bg-surface-strong',
			props.editing ? 'border-[rgba(12,118,98,0.3)] shadow-[0_14px_34px_rgba(12,118,98,0.08)]' : 'border-line',
		]"
	>
		<summary class="collapse-title flex items-start justify-between gap-3 p-2">
			<div class="flex items-center">
				<span
					:class="[
						'badge uppercase mr-3',
						props.alias.routable
							? 'border border-accent-soft bg-accent-soft text-accent'
							: 'border border-[rgba(164,63,63,0.18)] bg-danger-soft text-danger',
					]"
				>
					{{ props.alias.routable ? 'Routable' : 'Unavailable' }}
				</span>
				<span class="badge font-light">{{ props.alias.aliasModelId }}</span>
				<ArrowRight class="size-3 mx-1" />
				<span class="badge font-light">
					{{ props.alias.targetModelId }}
					<template v-if="props.alias.targetProviderName"> on {{ props.alias.targetProviderName }}</template>
					<template v-else> on any enabled provider</template>
				</span>
			</div>
		</summary>

		<div class="collapse-content space-y-4 mt-2">
			<div v-if="props.alias.providers.length > 0" class="flex flex-wrap gap-2.5 text-sm items-baseline">
				<span class="font-mono">Available on</span>
				<span v-for="provider in props.alias.providers" :key="provider.id" class="badge border-accent-soft bg-[rgba(12,118,98,0.08)]">
					{{ provider.name }}
				</span>
			</div>
			<p v-else class="rounded-[16px] border border-dashed border-line bg-white/50 px-3.5 py-4 leading-[1.6] text-ink-soft">
				No enabled provider currently routes this alias.
			</p>

			<div class="flex flex-wrap items-center justify-start gap-3 max-lg:flex-col max-lg:items-stretch">
				<button class="btn btn-sm" type="button" @click="emit('edit')">Edit</button>
				<button class="btn btn-danger btn-sm" type="button" @click="emit('delete')">Delete</button>
			</div>
		</div>
	</details>
</template>
