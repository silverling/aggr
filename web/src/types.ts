export type NoticeTone = 'success' | 'error' | 'info'

export interface ProviderView {
	id: number
	name: string
	baseUrl: string
	userAgent?: string
	enabled: boolean
	models: string[]
	disabledModels: string[]
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

export interface ModelAliasView {
	id: number
	aliasModelId: string
	targetModelId: string
	targetProviderId?: number
	targetProviderName?: string
	providers: ModelProviderSummary[]
	routable: boolean
	createdAt: string
	updatedAt: string
}

export interface ModelDisableRuleSelection {
	providerId: number
	providerName: string
	modelId: string
}

export interface ProvidersPayload {
	providers: ProviderView[]
}

export interface ModelsPayload {
	models: ModelRoute[]
}

export interface ModelAliasesPayload {
	aliases: ModelAliasView[]
}

export interface ProxyRequestLogView {
	id: number
	providerId?: number
	providerName?: string
	modelId?: string
	receivedRequest: ProxyRequestReceivedRequestView
	sentRequest?: ProxyRequestSentRequestView
	receivedResponse: ProxyRequestReceivedResponseView
	durationMs?: number
	requestedAt: string
	completedAt?: string
}

export interface ProxyRequestReceivedRequestView {
	method: string
	path: string
	rawQuery?: string
	headers: string
	body?: string
	bodyTruncated: boolean
}

export interface ProxyRequestSentRequestView {
	method: string
	url: string
	headers: string
	body?: string
	bodyTruncated: boolean
}

export interface ProxyRequestReceivedResponseView {
	status?: number
	headers?: string
	body?: string
	bodyTruncated: boolean
	error?: string
}

export interface ProxyRequestsPayload {
	requests: ProxyRequestLogView[]
}

export interface DeleteProxyRequestsPayload {
	deleted: number
}

export interface SetModelDisableRulePayload {
	providerId: number
	modelId: string
	disabled: boolean
}
