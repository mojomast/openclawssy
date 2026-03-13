import { Link, Outlet, useLocation } from 'react-router-dom'
import { Button } from './ui/button'
import { useTheme } from '../hooks/useTheme'

const navItems = [
  { path: '/help', label: 'Help' },
  { path: '/workspace', label: 'Workspace' },
  { path: '/secrets', label: 'Secrets' },
  { path: '/docs', label: 'Docs' },
  { path: '/skills', label: 'Skills' },
]

export function Layout() {
  const location = useLocation()
  const { theme, toggleTheme } = useTheme()

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
            {theme === 'dark' ? 'Light' : 'Dark'}
          </Button>
        </div>
      </header>

      {/* Main Content */}
      <div className="flex-1 flex">
        {/* Sidebar Navigation */}
        <nav className="w-48 border-r bg-card p-4 flex flex-col gap-1">
          {navItems.map((item) => {
            const isActive = location.pathname === item.path
            return (
              <Link
                key={item.path}
                to={item.path}
                className={`px-3 py-2 rounded-md text-sm transition-colors ${
                  isActive
                    ? 'bg-primary text-primary-foreground'
                    : 'hover:bg-muted'
                }`}
              >
                {item.label}
              </Link>
            )
          })}
        </nav>

        {/* Main Area */}
        <main className="flex-1 p-6 overflow-auto">
          <Outlet />
        </main>

        {/* Inspector Panel */}
        <aside className="w-64 border-l bg-card p-4 hidden lg:block">
          <h3 className="text-sm font-medium mb-2">Inspector</h3>
          <p className="text-xs text-muted-foreground">
            Inspector panel placeholder
          </p>
        </aside>
      </div>

      {/* Footer */}
      <footer className="border-t bg-card px-4 py-2 text-xs text-muted-foreground">
        <a href="/dashboard-legacy" className="hover:underline">
          Open Legacy Dashboard
        </a>
      </footer>
    </div>
  )
}
