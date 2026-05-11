export type NoticeTone = 'success' | 'error' | 'info'

export interface ProviderView {
	id: number
	name: string
	baseUrl: string
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
