<script setup lang="ts">
import type { ModelRoute } from '../types'

const props = defineProps<{
	model: ModelRoute
}>()
</script>

<template>
	<article data-anchor="model-card" class="grid gap-3.5 rounded-card border border-line bg-surface-strong p-4.5">
		<h3 class="font-mono text-[0.98rem] wrap-break-word">{{ props.model.id }}</h3>
		<div class="flex flex-wrap gap-2">
			<span
				v-for="provider in props.model.providers"
				:key="provider.id"
				class="inline-flex items-center rounded-full border border-[rgba(200,93,53,0.14)] bg-[rgba(200,93,53,0.08)] px-3 py-2 font-mono text-[0.82rem] font-bold text-accent-strong"
			>
				{{ provider.name }}
			</span>
		</div>
	</article>
</template>
