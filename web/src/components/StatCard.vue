<script setup lang="ts">
const props = defineProps<{
	label: string
	value: string | number
	description: string
}>()
</script>

<template>
	<article class="stat-card">
		<p>{{ props.label }}</p>
		<strong>{{ props.value }}</strong>
		<span>{{ props.description }}</span>
	</article>
</template>
