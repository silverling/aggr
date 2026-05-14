<script setup lang="ts">
import type { PendingModelDisableRule } from '../types'

const props = defineProps<{
	rules: PendingModelDisableRule[]
	applying: boolean
}>()

const emit = defineEmits<{
	(e: 'remove-rule', rule: PendingModelDisableRule): void
	(e: 'clear'): void
	(e: 'apply'): void
}>()

// actionLabel converts the pending disabled state into the UI label shown for
// one staged rule.
function actionLabel(rule: PendingModelDisableRule) {
	return rule.disabled ? 'Disable' : 'Re-enable'
}
</script>

<template>
	<aside
		v-if="props.rules.length > 0"
		data-anchor="model-disable-rule-pending"
		class="absolute bottom-0 -right-76 top-0 z-20 hidden w-70 min-[1848px]:block"
		aria-label="Pending model disable rule changes"
	>
		<div
			class="sticky top-6 w-70 rounded-3xl border border-[rgba(24,34,47,0.12)] bg-[rgba(255,252,247,0.78)] p-4 shadow-[0_20px_48px_rgba(22,34,49,0.12)] backdrop-blur-[18px]"
		>
			<div class="grid gap-2">
				<div class="flex items-start justify-between gap-3">
					<div>
						<p class="eyebrow">Pending changes</p>
						<h3 class="mt-1 text-[1rem]">Model disable rules</h3>
					</div>
					<span class="rounded-full bg-white/80 px-2.5 py-1 font-mono text-[0.72rem] font-bold text-ink-strong">
						{{ props.rules.length }}
					</span>
				</div>
				<p class="text-sm leading-[1.6] text-ink-soft">Stage multiple provider/model route changes, then apply them together.</p>
			</div>

			<div class="mt-4 grid max-h-105 gap-2.5 overflow-y-auto pr-1">
				<div v-for="rule in props.rules" :key="`${rule.providerId}:${rule.modelId}`" class="rounded-[18px] border border-line bg-white/80 p-3">
					<div class="flex items-start justify-between gap-3">
						<div class="min-w-0">
							<p :class="['eyebrow', rule.disabled ? 'text-danger' : 'text-accent']">
								{{ actionLabel(rule) }}
							</p>
							<p class="mt-1 wrap-break-word font-mono text-[0.82rem] font-bold text-ink-strong">
								{{ rule.providerName }} · {{ rule.modelId }}
							</p>
						</div>
						<button
							class="btn btn-sm text-ink-soft transition duration-150 ease-out hover:text-ink-strong"
							type="button"
							@click="emit('remove-rule', rule)"
						>
							Remove
						</button>
					</div>
				</div>
			</div>

			<div class="mt-4 grid gap-2.5">
				<button class="btn btn-accent" type="button" :disabled="props.applying" @click="emit('apply')">
					{{ props.applying ? 'Applying…' : `Apply ${props.rules.length} change${props.rules.length === 1 ? '' : 's'}` }}
				</button>
				<button class="btn" type="button" :disabled="props.applying" @click="emit('clear')">Cancel</button>
			</div>
		</div>
	</aside>
</template>
