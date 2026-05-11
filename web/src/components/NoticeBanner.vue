<script setup lang="ts">
import type { NoticeTone } from '../types'

const props = defineProps<{
	tone: NoticeTone
	text: string
}>()

const toneClassMap: Record<NoticeTone, string> = {
	success: 'border-[rgba(12,118,98,0.18)] bg-accent-soft text-accent',
	error: 'border-[rgba(164,63,63,0.22)] bg-danger-soft text-danger',
	info: 'border-[rgba(24,34,47,0.12)] bg-[rgba(24,34,47,0.06)] text-ink',
}
</script>

<template>
	<div data-anchor="notice" :class="['rounded-[18px] border px-4.5 py-3.5 font-semibold', toneClassMap[props.tone]]">
		{{ props.text }}
	</div>
</template>
