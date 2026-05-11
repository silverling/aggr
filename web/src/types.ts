export type NoticeTone = 'success' | 'error' | 'info'

export interface ProviderView {
	id: number
	name: string
	baseUrl: string
	userAgent?: string
	enabled: boolean
	models: string[]
	lastError?: string
	lastSyncedAt?: string
	apiKeyConfigured: boolean
	apiKeyPreview?: string
}

export interface ModelProviderSummary {
	id: number
	name: string
}

export interface ModelRoute {
	id: string
	providers: ModelProviderSummary[]
}

export interface ProvidersPayload {
	providers: ProviderView[]
}

export interface ModelsPayload {
	models: ModelRoute[]
}

export interface ProxyRequestLogView {
	id: number
	providerId?: number
	providerName?: string
	modelId?: string
	method: string
	path: string
	rawQuery?: string
	requestHeaders: string
	requestBody?: string
	requestBodyTruncated: boolean
	responseStatus?: number
	responseHeaders?: string
	responseBody?: string
	responseBodyTruncated: boolean
	error?: string
	durationMs?: number
	requestedAt: string
	completedAt?: string
}

export interface ProxyRequestsPayload {
	requests: ProxyRequestLogView[]
}

export interface DeleteProxyRequestsPayload {
	deleted: number
}
