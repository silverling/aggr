export type NoticeTone = 'success' | 'error' | 'info'

export interface AuthSessionView {
	id: number
	userAgent?: string
	remoteAddr?: string
	createdAt: string
	lastSeenAt: string
	current: boolean
}

export interface AuthSessionStateResponse {
	authenticated: boolean
	version: string
	session?: AuthSessionView
}

export interface AuthSessionsPayload {
	sessions: AuthSessionView[]
}

export interface GatewayApiKeyView {
	id: number
	name: string
	keyPrefix: string
	createdAt: string
	lastUsedAt?: string
}

export interface GatewayApiKeysPayload {
	apiKeys: GatewayApiKeyView[]
}

export interface CreateGatewayApiKeyResponse {
	apiKey: string
	key: GatewayApiKeyView
}

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

export interface PendingModelDisableRule {
	providerId: number
	providerName: string
	modelId: string
	disabled: boolean
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

export interface ProxyRequestLogSummaryView {
	id: number
	providerId?: number
	providerName?: string
	modelId?: string
	method: string
	path: string
	rawQuery?: string
	status?: number
	error?: string
	durationMs?: number
	cachedInputTokens: number
	nonCachedInputTokens: number
	outputTokens: number
	totalTokens: number
	requestedAt: string
	completedAt?: string
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
	cachedInputTokens: number
	nonCachedInputTokens: number
	outputTokens: number
	totalTokens: number
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
	requests: ProxyRequestLogSummaryView[]
}

export interface RequestStatsSummary {
	requests: number
	succeeded: number
	failed: number
	consumedTokens: number
	cachedInputTokens: number
	nonCachedInputTokens: number
	outputTokens: number
	ongoingRequests: number
}

export interface RequestStatsBucket {
	start: string
	label: string
	requests: number
	succeeded: number
	failed: number
	consumedTokens: number
	cachedInputTokens: number
	nonCachedInputTokens: number
	outputTokens: number
}

export interface RequestStatsView {
	range: string
	rangeLabel: string
	summary: RequestStatsSummary
	daily: RequestStatsBucket[]
	hourly: RequestStatsBucket[]
}

export interface StatsRangeOption {
	value: string
	label: string
}

export interface DeleteProxyRequestsPayload {
	deleted: number
}

export interface SetModelDisableRuleItemPayload {
	providerId: number
	modelId: string
	disabled: boolean
}

export interface SetModelDisableRulePayload {
	rules: SetModelDisableRuleItemPayload[]
}

export interface SectionOutlineItem {
	anchor: string
	label: string
	shortLabel: string
}
