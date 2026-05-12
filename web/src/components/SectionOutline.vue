<script setup lang="ts">
import type { SectionOutlineItem } from '../types'
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = defineProps<{
	items: SectionOutlineItem[]
}>()

const activeAnchor = ref(props.items[0]?.anchor ?? '')
const observedElements = new Map<Element, string>()
let observer: IntersectionObserver | null = null

// scrollToAnchor jumps to the requested dashboard section while keeping the
// floating outline interaction local to this component.
function scrollToAnchor(anchor: string) {
	const element = document.querySelector<HTMLElement>(`[data-anchor="${anchor}"]`)
	if (element === null) {
		return
	}

	element.scrollIntoView({
		behavior: 'smooth',
		block: 'start',
	})
}

// disconnectObserver tears down the active intersection observer so component
// re-renders and unmounts do not leak browser observers.
function disconnectObserver() {
	observer?.disconnect()
	observer = null
	observedElements.clear()
}

// connectObserver attaches a scrollspy observer to every declared section
// anchor and keeps the active item in sync with the section nearest the top.
function connectObserver() {
	disconnectObserver()

	const entriesByAnchor = new Map<string, IntersectionObserverEntry>()
	observer = new IntersectionObserver(
		(entries) => {
			for (const entry of entries) {
				const anchor = observedElements.get(entry.target)
				if (anchor === undefined) {
					continue
				}

				if (entry.isIntersecting) {
					entriesByAnchor.set(anchor, entry)
					continue
				}

				entriesByAnchor.delete(anchor)
			}

			const visibleEntries = [...entriesByAnchor.values()].sort((left, right) => {
				if (left.boundingClientRect.top === right.boundingClientRect.top) {
					return right.intersectionRatio - left.intersectionRatio
				}

				return left.boundingClientRect.top - right.boundingClientRect.top
			})

			if (visibleEntries.length > 0) {
				const anchor = observedElements.get(visibleEntries[0].target)
				if (anchor !== undefined) {
					activeAnchor.value = anchor
				}
			}
		},
		{
			rootMargin: '-18% 0px -62% 0px',
			threshold: [0.1, 0.3, 0.6],
		},
	)

	for (const item of props.items) {
		const element = document.querySelector<HTMLElement>(`[data-anchor="${item.anchor}"]`)
		if (element === null || observer === null) {
			continue
		}

		observedElements.set(element, item.anchor)
		observer.observe(element)
	}
}

onMounted(() => {
	connectObserver()
})

onBeforeUnmount(() => {
	disconnectObserver()
})

watch(
	() => props.items,
	() => {
		connectObserver()
	},
	{ deep: true },
)
</script>

<template>
	<aside class="fixed left-6 top-6 z-20 hidden min-[1848px]:block" aria-label="Dashboard section outline">
		<div
			class="w-[220px] rounded-[24px] border border-[rgba(24,34,47,0.12)] bg-[rgba(255,252,247,0.64)] p-3 shadow-[0_20px_48px_rgba(22,34,49,0.1)] backdrop-blur-[18px]"
		>
			<p class="px-2.5 pb-2 text-[0.72rem] font-bold uppercase tracking-[0.18em] text-accent-strong">Outline</p>
			<nav class="grid gap-1.5">
				<button
					v-for="item in props.items"
					:key="item.anchor"
					:class="[
						'group flex w-full items-center justify-between gap-3 rounded-[18px] px-2.5 py-2.5 text-left transition duration-150 ease-out',
						activeAnchor === item.anchor
							? 'bg-[linear-gradient(135deg,rgba(12,118,98,0.14),rgba(200,93,53,0.09))] text-ink-strong shadow-[inset_0_0_0_1px_rgba(12,118,98,0.18)]'
							: 'text-ink-soft hover:bg-white/60 hover:text-ink-strong',
					]"
					type="button"
					@click="scrollToAnchor(item.anchor)"
				>
					<span class="flex items-center gap-2.5">
						<span
							:class="[
								'h-2 w-2 rounded-full transition duration-150 ease-out',
								activeAnchor === item.anchor
									? 'bg-accent shadow-[0_0_0_5px_rgba(12,118,98,0.12)]'
									: 'bg-[rgba(24,34,47,0.16)] group-hover:bg-accent/70',
							]"
						/>
						<span class="text-[0.94rem] font-bold leading-[1.25]">{{ item.label }}</span>
					</span>
					<span
						:class="[
							'text-[0.72rem] uppercase tracking-[0.18em] transition duration-150 ease-out',
							activeAnchor === item.anchor ? 'text-accent' : 'text-ink-soft/70 group-hover:text-accent',
						]"
					>
						{{ item.shortLabel }}
					</span>
				</button>
			</nav>
		</div>
	</aside>
</template>
