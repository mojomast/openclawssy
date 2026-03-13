import { HashRouter, Routes, Route } from 'react-router-dom'
import { Layout } from './components/Layout'
import { HelpPage } from './pages/HelpPage'
import { WorkspacePage } from './pages/WorkspacePage'
import { SecretsPage } from './pages/SecretsPage'
import { DocsPage } from './pages/DocsPage'
import { SkillsPage } from './pages/SkillsPage'

function App() {
  return (
    <HashRouter>
      <Routes>
        <Route path="/" element={<Layout />}>
          <Route index element={<HelpPage />} />
          <Route path="help" element={<HelpPage />} />
          <Route path="workspace" element={<WorkspacePage />} />
          <Route path="secrets" element={<SecretsPage />} />
          <Route path="docs" element={<DocsPage />} />
          <Route path="skills" element={<SkillsPage />} />
        </Route>
      </Routes>
    </HashRouter>
  )
}

export default App
