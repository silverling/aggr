<script setup lang="ts">
import type { NoticeTone } from '../types'

const props = defineProps<{
	tone: NoticeTone
	text: string
}>()
</script>

<template>
	<div :class="['notice', `notice-${props.tone}`]">
		{{ props.text }}
	</div>
</template>
