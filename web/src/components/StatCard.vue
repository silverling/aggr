<script setup lang="ts">
import { formatCompactNumber } from '../lib/utils'

const props = defineProps<{
	label: string
	value: number
	description: string
}>()
</script>

<template>
	<article data-anchor="stat-card" class="grid gap-2 rounded-card border border-line bg-surface-strong p-5">
		<p class="m-0 text-sm text-ink-soft">{{ props.label }}</p>
		<strong class="text-[2.1rem] leading-none text-ink-strong">{{ formatCompactNumber(props.value) }}</strong>
		<span class="text-ink-soft">{{ props.description }}</span>
	</article>
</template>
