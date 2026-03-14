import { HashRouter, Routes, Route } from 'react-router-dom'
import { Layout } from './components/Layout'
import { AuthTokenGate } from './components/AuthTokenGate'
import { Toaster } from './components/ui/toaster'
import {
  // Migrated pages
  HelpPage,
  WorkspacePage,
  SecretsPage,
  DocsPage,
  SkillsPage,
  // Placeholder pages (remaining migrations)
  ChatPage,
  RunsPage,
  SessionsPage,
  AgentMonitorPage,
  SchedulerPage,
  SandboxPage,
  DashboardsPage,
  // New pages (control plane)
  AgentContractPage,
  PromptStackPage,
  RoleTemplatePage,
  DelegationPage,
  EvalPage,
} from './pages'
import { SettingsPage } from './pages/SettingsPage'

function App() {
  return (
    <HashRouter>
      <Routes>
        <Route path="/" element={<Layout />}>
          {/* 13 migrated pages */}
          <Route index element={<HelpPage />} />
          <Route path="help" element={<HelpPage />} />
          <Route path="workspace" element={<WorkspacePage />} />
          <Route path="secrets" element={<SecretsPage />} />
          <Route path="docs" element={<DocsPage />} />
          <Route path="skills" element={<SkillsPage />} />
          <Route path="chat" element={<ChatPage />} />
          <Route path="settings" element={<SettingsPage />} />
          <Route path="runs" element={<RunsPage />} />
          <Route path="runs/:runId" element={<RunsPage />} />
          <Route path="sessions" element={<SessionsPage />} />
          <Route path="monitor" element={<AgentMonitorPage />} />
          <Route path="scheduler" element={<SchedulerPage />} />
          <Route path="sandbox" element={<SandboxPage />} />
          <Route path="dashboards" element={<DashboardsPage />} />

          {/* 5 new control plane pages */}
          <Route path="agent-contract" element={<AgentContractPage />} />
          <Route path="prompt-stack" element={<PromptStackPage />} />
          <Route path="roles" element={<RoleTemplatePage />} />
          <Route path="delegation" element={<DelegationPage />} />
          <Route path="eval" element={<EvalPage />} />
        </Route>
      </Routes>
      <AuthTokenGate />
      <Toaster />
    </HashRouter>
  )
}

export default App
