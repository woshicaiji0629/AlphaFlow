import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { BrowserRouter, Navigate, Route, Routes } from 'react-router-dom'
import { App } from './App'
import { ProtectedRoute } from './auth/ProtectedRoute'
import { LoginPage } from './pages/LoginPage'
import { StrategiesPage } from './pages/StrategiesPage'
import { StrategyPerformancePage } from './pages/StrategyPerformancePage'
import { AdminStrategiesPage } from './pages/AdminStrategiesPage'
import './styles.css'

const queryClient = new QueryClient({ defaultOptions: { queries: { refetchOnWindowFocus: false } } })

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <BrowserRouter>
        <Routes>
          <Route path="/login" element={<LoginPage/>}/>
          <Route element={<ProtectedRoute/>}><Route path="/" element={<App/>}/><Route path="/strategies" element={<StrategiesPage/>}/><Route path="/strategies/:id/performance" element={<StrategyPerformancePage/>}/><Route path="/admin/strategies" element={<AdminStrategiesPage/>}/></Route>
          <Route path="*" element={<Navigate to="/" replace/>}/>
        </Routes>
      </BrowserRouter>
    </QueryClientProvider>
  </StrictMode>,
)
