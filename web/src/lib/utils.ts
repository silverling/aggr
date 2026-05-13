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
