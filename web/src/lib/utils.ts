const compactNumberFormatter = new Intl.NumberFormat(undefined, {
	maximumFractionDigits: 1,
	notation: 'compact',
})

export function formatCompactNumber(value: number) {
	return compactNumberFormatter.format(value)
}

export function formatDuration(ms: number) {
	if (ms < 1000) {
		return `${ms}ms`
	} else {
		return `${(ms / 1000).toFixed(1)}s`
	}
}

export function formatTokenCount(value: number) {
	return value.toLocaleString()
}

export function formatCacheRate(cachedTokens: number, totalTokens: number) {
	if (totalTokens === 0) {
		return '0%'
	}
	const rate = (cachedTokens / totalTokens) * 100
	return `${rate.toFixed(1)}%`
}

const dateFormatter = new Intl.DateTimeFormat(undefined, {
	dateStyle: 'medium',
	timeStyle: 'medium',
})

export function formatTimestamp(value?: string) {
	if (!value) {
		return 'Pending'
	}

	const parsed = new Date(value)
	if (Number.isNaN(parsed.valueOf())) {
		return value
	}

	return dateFormatter.format(parsed)
}

export function formatPath(path: string, rawQuery?: string) {
	if (!rawQuery) {
		return path
	}

	return `${path}?${rawQuery}`
}

export function isJSONContentType(value: string) {
	return value === 'application/json' || value.endsWith('+json')
}

export function scrollToAnchor(anchor: string, offset = 0) {
	const el = document.querySelector(`[data-anchor="${anchor}"]`)
	if (el) {
		window.scrollTo({ top: el.getBoundingClientRect().top + window.scrollY - offset, behavior: 'smooth' })
	}
}

export function prettyJSONString(value: string) {
	try {
		return JSON.stringify(JSON.parse(value), null, 2)
	} catch {
		return value
	}
}
