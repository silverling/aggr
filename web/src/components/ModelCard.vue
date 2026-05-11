<script setup lang="ts">
import type { ModelRoute } from '../types'

const props = defineProps<{
	model: ModelRoute
}>()
</script>

<template>
	<article class="model-card">
		<h3>{{ props.model.id }}</h3>
		<div class="chip-row chip-row-compact">
			<span v-for="provider in props.model.providers" :key="provider.id" class="chip provider-chip">
				{{ provider.name }}
			</span>
		</div>
	</article>
</template>
