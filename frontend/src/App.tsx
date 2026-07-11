import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from 'react-router-dom'
import { getDashboard } from './api/client'
import type { DashboardSnapshot, HealthStatus, MarketPoint } from './api/types'
import { useAuth } from './auth/AuthProvider'

const navigation = [
	{ label: '总览', icon: '⌂', roles: ['admin', 'user'] },
	{ label: '我的账户', icon: '◎', roles: ['admin', 'user'] },
	{ label: '我的仓位', icon: '▤', roles: ['admin', 'user'] },
	{ label: '我的订单', icon: '⌁', roles: ['admin', 'user'] },
	{ label: '可用策略', icon: '◇', roles: ['admin', 'user'], path: '/strategies' },
	{ label: '策略表现', icon: '↗', roles: ['admin', 'user'], path: '/strategies' },
	{ label: '策略管理', icon: '◆', roles: ['admin'], path: '/admin/strategies' },
	{ label: '官方回测', icon: '◫', roles: ['admin'] },
	{ label: '用户管理', icon: '♙', roles: ['admin'] },
	{ label: '系统状态', icon: '⚙', roles: ['admin'] },
	{ label: '审计日志', icon: '≡', roles: ['admin'] },
] as const

function EquityChart({ points }: { points: MarketPoint[] }) {
  const path = useMemo(() => {
    const values = points.map((point) => point.value)
    const min = Math.min(...values)
    const max = Math.max(...values)
    const range = max - min || 1
    return points.map((point, index) => {
      const x = (index / (points.length - 1)) * 100
      const y = 92 - ((point.value - min) / range) * 78
      return `${index === 0 ? 'M' : 'L'} ${x} ${y}`
    }).join(' ')
  }, [points])

	if (points.length < 2) return <div className="chart-empty">权益数据尚未接入</div>
  return <svg className="equity-chart" viewBox="0 0 100 100" preserveAspectRatio="none" aria-label="组合净值曲线">
    <defs><linearGradient id="fill" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stopColor="#54e0a3" stopOpacity=".3"/><stop offset="1" stopColor="#54e0a3" stopOpacity="0"/></linearGradient></defs>
    <path d={`${path} L 100 100 L 0 100 Z`} fill="url(#fill)" />
    <path d={path} fill="none" stroke="#54e0a3" strokeWidth="1.1" vectorEffect="non-scaling-stroke" />
  </svg>
}

function StatusDot({ status }: { status: HealthStatus }) {
  return <span className={`status-dot ${status}`} aria-label={status} />
}

export function App() {
	const { user, signOut, signingOut } = useAuth()
	const navigate = useNavigate()
	const nav = navigation.filter((item) => item.roles.some((role) => role === user.role))
	const dashboard = useQuery({ queryKey: ['dashboard', user.id], queryFn: ({ signal }) => getDashboard(signal), refetchInterval: 15_000 })
	if (dashboard.isError) return <main className="state-page"><p>无法加载量化工作台</p><small>{dashboard.error.message}</small></main>
	if (!dashboard.data) return <main className="state-page"><span className="loader"/><p>正在同步策略状态…</p></main>
	const data: DashboardSnapshot = dashboard.data

  return <div className="shell">
    <aside className="sidebar">
      <div className="brand"><span className="brand-mark">A</span><div><strong>ALPHAFLOW</strong><small>QUANT CONSOLE</small></div></div>
      <nav>{nav.map((item, index) => <button key={item.label} className={index === 0 ? 'active' : ''} onClick={() => { if ('path' in item) navigate(item.path) }}><span>{item.icon}</span>{item.label}</button>)}</nav>
      <div className="sidebar-foot"><StatusDot status="healthy"/><div><strong>系统运行中</strong><small>所有队列已连接</small></div></div>
    </aside>
    <main className="content">
      <header><div><p className="eyebrow">OPERATIONS / OVERVIEW</p><h1>量化交易总览</h1></div><div className="header-actions"><span className="role-label">{user.role === 'admin' ? '管理员' : '普通用户'}</span><span className="mode">{data.mode.toUpperCase()}</span><button className="logout-button" disabled={signingOut} onClick={() => void signOut()}>{signingOut ? '退出中' : '退出'}</button><div className="avatar" title={user.email}>{user.display_name.slice(0, 2).toUpperCase()}</div></div></header>
      <section className="metrics">{data.metrics.map((metric) => <article className="metric" key={metric.label}><span>{metric.label}</span><strong>{metric.value}</strong><small className={metric.trend}>{metric.change}</small></article>)}</section>
      <section className="dashboard-grid">
        <article className="panel chart-panel"><div className="panel-head"><div><p className="eyebrow">PORTFOLIO EQUITY</p><h2>组合净值</h2></div><div className="periods"><button>1D</button><button className="active">1W</button><button>1M</button></div></div><div className="chart-value">{data.equity.length ? `$${data.equity.at(-1)!.value.toLocaleString()}` : '—'}</div><EquityChart points={data.equity}/><div className="chart-axis"><span>07/05</span><span>07/07</span><span>07/09</span><span>今天</span></div></article>
        <article className="panel service-panel"><div className="panel-head"><div><p className="eyebrow">INFRASTRUCTURE</p><h2>服务状态</h2></div><button className="text-button">查看全部</button></div><div className="service-list">{data.services.map((service) => <div className="service" key={service.id}><StatusDot status={service.status}/><div><strong>{service.name}</strong><small>{service.detail}</small></div><span>{service.status === 'healthy' ? '正常' : '降级'}</span></div>)}</div></article>
        <article className="panel positions"><div className="panel-head"><div><p className="eyebrow">OPEN POSITIONS</p><h2>活跃仓位</h2></div><button className="text-button">仓位详情</button></div><div className="table-wrap"><table><thead><tr><th>交易对</th><th>方向</th><th>策略</th><th>入场 / 标记</th><th>杠杆</th><th>未实现盈亏</th></tr></thead><tbody>{data.positions.map((position) => <tr key={position.id}><td><strong>{position.symbol}</strong><small>{position.account ?? 'PERP'}</small></td><td><span className={`side ${position.side}`}>{position.side === 'long' ? 'LONG' : 'SHORT'}</span></td><td>{position.strategy}</td><td>{position.entryPrice.toLocaleString()}<small>{position.markPrice == null ? '标记价未接入' : position.markPrice.toLocaleString()}</small></td><td>{position.leverage > 0 ? `${position.leverage}×` : '—'}</td><td className={position.pnl == null ? '' : position.pnl >= 0 ? 'profit' : 'loss'}>{position.pnl == null ? '—' : `${position.pnl >= 0 ? '+' : ''}$${position.pnl.toFixed(2)}`}<small>{position.pnlPercent == null ? '实时盈亏未接入' : `${position.pnlPercent.toFixed(2)}%`}</small></td></tr>)}</tbody></table></div></article>
        <article className="panel signals"><div className="panel-head"><div><p className="eyebrow">LATEST SIGNALS</p><h2>策略信号</h2></div><span className="live-label">{data.dataStatus?.signals === 'not_configured' ? '未接入' : <><i/> LIVE</>}</span></div>{data.signals.length ? data.signals.map((signal) => <div className="signal" key={signal.id}><time>{signal.time}</time><span className={`signal-tag ${signal.signal}`}>{signal.signal.toUpperCase()}</span><div><strong>{signal.symbol} · {signal.strategy}</strong><small>{signal.reason}</small></div><b>{signal.confidence}%</b></div>) : <div className="chart-empty">暂无可见策略信号</div>}</article>
      </section>
      <footer>数据时间 {new Date(data.asOf).toLocaleString('zh-CN')} · 个人账户隔离数据 · 其他用户数据默认不可见</footer>
    </main>
  </div>
}
