import type { DashboardSnapshot } from './types'

export const dashboardMock: DashboardSnapshot = {
  asOf: '2026-07-11T09:42:18+08:00',
  mode: 'paper',
  metrics: [
    { label: '组合净值', value: '$12,684.20', change: '+2.84%', trend: 'up' },
    { label: '今日盈亏', value: '+$348.62', change: '+1.16%', trend: 'up' },
    { label: '最大回撤', value: '4.73%', change: '-0.31%', trend: 'down' },
    { label: '活跃策略', value: '3 / 4', change: '1 degraded', trend: 'flat' },
  ],
  services: [
    { id: 'market-data', name: 'Market Data', detail: '延迟 84ms · 4 exchanges', status: 'healthy' },
    { id: 'strategy-engine', name: 'Strategy Engine', detail: 'Supertrend · 42 symbols', status: 'healthy' },
    { id: 'position-engine', name: 'Position Engine', detail: 'Paper route active', status: 'healthy' },
    { id: 'clickhouse', name: 'ClickHouse', detail: 'Last write 3s ago', status: 'degraded' },
  ],
  positions: [
    { id: 'p1', symbol: 'BTCUSDT', strategy: 'supertrend', side: 'long', leverage: 10, entryPrice: 118420.5, markPrice: 119284.2, pnl: 72.94, pnlPercent: 7.29 },
    { id: 'p2', symbol: 'ETHUSDT', strategy: 'momentum', side: 'short', leverage: 8, entryPrice: 3628.4, markPrice: 3601.8, pnl: 31.18, pnlPercent: 3.12 },
    { id: 'p3', symbol: 'SOLUSDT', strategy: 'supertrend', side: 'long', leverage: 6, entryPrice: 162.31, markPrice: 161.78, pnl: -9.74, pnlPercent: -0.97 },
  ],
  signals: [
    { id: 's1', time: '09:41:36', symbol: 'BTCUSDT', strategy: 'supertrend', signal: 'long', confidence: 86, reason: '3m 趋势翻多，多周期动量确认' },
    { id: 's2', time: '09:38:14', symbol: 'ETHUSDT', strategy: 'momentum', signal: 'hold', confidence: 64, reason: '成交量确认不足，保持当前仓位' },
    { id: 's3', time: '09:35:02', symbol: 'SOLUSDT', strategy: 'supertrend', signal: 'close', confidence: 78, reason: '移动止损触发条件接近阈值' },
  ],
  equity: [10000, 10120, 10084, 10340, 10290, 10610, 10880, 10740, 11220, 11640, 11520, 12010, 12340, 12260, 12684].map((value, index) => ({ time: `${index}`, value })),
}
