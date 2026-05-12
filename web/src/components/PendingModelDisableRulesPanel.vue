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
		class="absolute right-0 top-6 z-20 hidden min-[1848px]:block translate-x-[calc(100%+24px)]"
		aria-label="Pending model disable rule changes"
	>
		<div
			class="w-[280px] rounded-[24px] border border-[rgba(24,34,47,0.12)] bg-[rgba(255,252,247,0.78)] p-4 shadow-[0_20px_48px_rgba(22,34,49,0.12)] backdrop-blur-[18px]"
		>
			<div class="grid gap-2">
				<div class="flex items-start justify-between gap-3">
					<div>
						<p class="text-[0.72rem] font-bold uppercase tracking-[0.18em] text-accent-strong">Pending changes</p>
						<h3 class="mt-1 text-[1rem]">Model disable rules</h3>
					</div>
					<span class="rounded-full bg-white/80 px-2.5 py-1 font-mono text-[0.72rem] font-bold text-ink-strong">
						{{ props.rules.length }}
					</span>
				</div>
				<p class="text-sm leading-[1.6] text-ink-soft">Stage multiple provider/model route changes, then apply them together.</p>
			</div>

			<div class="mt-4 grid max-h-[420px] gap-2.5 overflow-y-auto pr-1">
				<div v-for="rule in props.rules" :key="`${rule.providerId}:${rule.modelId}`" class="rounded-[18px] border border-line bg-white/80 p-3">
					<div class="flex items-start justify-between gap-3">
						<div class="min-w-0">
							<p :class="['text-[0.72rem] font-bold uppercase tracking-[0.16em]', rule.disabled ? 'text-danger' : 'text-accent']">
								{{ actionLabel(rule) }}
							</p>
							<p class="mt-1 break-words font-mono text-[0.82rem] font-bold text-ink-strong">{{ rule.providerName }} · {{ rule.modelId }}</p>
						</div>
						<button
							class="border-0 bg-transparent p-0 text-sm font-bold text-ink-soft transition duration-150 ease-out hover:text-ink-strong"
							type="button"
							@click="emit('remove-rule', rule)"
						>
							Remove
						</button>
					</div>
				</div>
			</div>

			<div class="mt-4 grid gap-2.5">
				<button
					class="inline-flex min-h-12 items-center justify-center rounded-full border border-transparent bg-[linear-gradient(135deg,var(--color-accent),#0f9275)] px-[18px] font-bold text-[#f7fffc] transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60"
					type="button"
					:disabled="props.applying"
					@click="emit('apply')"
				>
					{{ props.applying ? 'Applying…' : `Apply ${props.rules.length} change${props.rules.length === 1 ? '' : 's'}` }}
				</button>
				<button
					class="inline-flex min-h-12 items-center justify-center rounded-full border border-line bg-[rgba(255,255,255,0.72)] px-[18px] font-bold text-ink-strong transition duration-150 ease-out hover:-translate-y-px hover:shadow-[0_10px_24px_rgba(24,34,47,0.12)] disabled:cursor-not-allowed disabled:opacity-60"
					type="button"
					:disabled="props.applying"
					@click="emit('clear')"
				>
					Clear all
				</button>
			</div>
		</div>
	</aside>
</template>
