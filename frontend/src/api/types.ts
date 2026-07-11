export type HealthStatus = 'healthy' | 'degraded' | 'offline'
export type PositionSide = 'long' | 'short'

export interface Metric {
  label: string
  value: string
  change?: string
  trend?: 'up' | 'down' | 'flat'
}

export interface ServiceHealth {
  id: string
  name: string
  detail: string
  status: HealthStatus
}

export interface Position {
  id: string
  symbol: string
  strategy: string
  side: PositionSide
  leverage: number
  entryPrice: number
	markPrice?: number | null
	pnl?: number | null
	pnlPercent?: number | null
	account?: string
	scope?: string
}

export interface StrategySignal {
  id: string
  time: string
  symbol: string
  strategy: string
  signal: 'long' | 'short' | 'hold' | 'close'
  confidence: number
  reason: string
}

export interface MarketPoint {
  time: string
  value: number
}

export interface DashboardSnapshot {
  asOf: string
  mode: 'paper' | 'testnet' | 'live'
  metrics: Metric[]
  services: ServiceHealth[]
  positions: Position[]
  signals: StrategySignal[]
  equity: MarketPoint[]
	dataStatus?: Record<string, string>
}

export interface AuthUser {
  id: string
  email: string
  display_name: string
	role: 'admin' | 'user'
	permissions: string[]
}

export interface AuthResponse { user: AuthUser }
export interface PublishedStrategy { id:string;code:string;name:string;description:string;version:string;riskLevel:'low'|'medium'|'high';paperEnabled:boolean;liveEnabled:boolean }
export interface StrategyPerformance { id:string;strategyId:string;strategyVersion:string;exchange:string;market:string;symbolScope:unknown;interval:string;periodStart:string;periodEnd:string;metrics:Record<string,unknown>;equityPoints:Array<{time:string;value:number}>;publishedAt:string }
export type StrategyRisk = 'low'|'medium'|'high'
export type StrategyVisibility = 'public'|'restricted'|'admin_only'
export type AdminStrategyStatus = 'draft'|'published'|'disabled'
export interface AdminStrategy { id:string;code:string;name:string;description:string;version:string;parameters:Record<string,string>;status:AdminStrategyStatus;visibility:StrategyVisibility;riskLevel:StrategyRisk;paperEnabled:boolean;liveEnabled:boolean;createdAt:string;updatedAt:string }
export interface AdminStrategyInput { code:string;name:string;description:string;version:string;parameters:Record<string,string>;status:'draft'|'published';visibility:StrategyVisibility;riskLevel:StrategyRisk;paperEnabled:boolean;liveEnabled:false }
export interface StrategyParameterDefinition { type:'number'|'integer'|'string'|string;description:string;required:boolean }
export interface StrategyDefinition { code:string;parameters:Record<string,StrategyParameterDefinition> }
