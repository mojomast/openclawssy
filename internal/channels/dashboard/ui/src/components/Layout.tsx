import { Link, Outlet, useLocation } from 'react-router-dom'
import { Button } from './ui/button'
import { useUIStore } from '../stores'

interface NavSection {
  title: string
  items: { path: string; label: string }[]
}

const navSections: NavSection[] = [
  {
    title: 'Dashboard',
    items: [
      { path: '/help', label: 'Help' },
      { path: '/workspace', label: 'Workspace' },
      { path: '/secrets', label: 'Secrets' },
      { path: '/docs', label: 'Docs' },
      { path: '/skills', label: 'Skills' },
      { path: '/chat', label: 'Chat' },
      { path: '/settings', label: 'Settings' },
    ],
  },
  {
    title: 'Operations',
    items: [
      { path: '/runs', label: 'Runs' },
      { path: '/sessions', label: 'Sessions' },
      { path: '/monitor', label: 'Monitor' },
      { path: '/scheduler', label: 'Scheduler' },
      { path: '/sandbox', label: 'Sandbox' },
      { path: '/dashboards', label: 'Dashboards' },
    ],
  },
  {
    title: 'Control Plane',
    items: [
      { path: '/agent-contract', label: 'Agent Contract' },
      { path: '/prompt-stack', label: 'Prompt Stack' },
      { path: '/roles', label: 'Roles' },
      { path: '/delegation', label: 'Delegation' },
      { path: '/eval', label: 'Eval' },
    ],
  },
]

export function Layout() {
  const location = useLocation()
  const { theme, toggleTheme, sidebar, inspector, toggleInspector } = useUIStore()

  // Calculate effective theme
  const effectiveTheme = theme === 'system'
    ? (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
    : theme

  return (
    <div className="min-h-screen flex flex-col">
      {/* Header */}
      <header className="border-b bg-card px-4 py-3 flex items-center justify-between">
        <div className="flex items-center gap-2">
          <h1 className="text-lg font-semibold">Openclawssy Dashboard</h1>
          <span className="text-xs text-muted-foreground bg-muted px-2 py-1 rounded">
            React
          </span>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="ghost" size="sm" onClick={toggleTheme}>
            {effectiveTheme === 'dark' ? 'Light' : 'Dark'}
          </Button>
        </div>
      </header>

      {/* Main Content */}
      <div className="flex-1 flex overflow-hidden">
        {/* Sidebar Navigation */}
        {sidebar.isOpen && (
          <nav 
            className="border-r bg-card p-4 flex flex-col gap-4 overflow-y-auto"
            style={{ width: sidebar.width }}
          >
            {navSections.map((section) => (
              <div key={section.title}>
                <h3 className="text-xs font-semibold text-muted-foreground uppercase tracking-wider mb-2 px-3">
                  {section.title}
                </h3>
                <div className="flex flex-col gap-0.5">
                  {section.items.map((item) => {
                    const isActive = location.pathname === item.path ||
                      (item.path !== '/' && location.pathname.startsWith(item.path))
                    return (
                      <Link
                        key={item.path}
                        to={item.path}
                        className={`px-3 py-2 rounded-md text-sm transition-colors ${
                          isActive
                            ? 'bg-primary text-primary-foreground font-medium'
                            : 'hover:bg-muted text-muted-foreground hover:text-foreground'
                        }`}
                      >
                        {item.label}
                      </Link>
                    )
                  })}
                </div>
              </div>
            ))}
          </nav>
        )}

        {/* Main Area */}
        <main className="flex-1 p-6 overflow-auto">
          <Outlet />
        </main>

        {/* Inspector Panel */}
        {inspector.isOpen && (
          <aside 
            className="border-l bg-card p-4 hidden lg:block overflow-y-auto"
            style={{ width: inspector.width }}
          >
            <div className="flex items-center justify-between mb-2">
              <h3 className="text-sm font-medium">Inspector</h3>
              <Button variant="ghost" size="sm" onClick={toggleInspector}>
                ×
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              Inspector panel placeholder
            </p>
          </aside>
        )}
      </div>

      {/* Footer */}
      <footer className="border-t bg-card px-4 py-2 text-xs text-muted-foreground flex items-center justify-between">
        <a href="/dashboard-legacy" className="hover:underline">
          Open Legacy Dashboard
        </a>
        <span>18 routes configured</span>
      </footer>
    </div>
  )
}
