import { Link, Outlet, useLocation, useNavigate } from 'react-router-dom'
import { Button } from './ui/button'
import { useUIStore } from '../stores'
import { useKeyboardShortcuts } from '../hooks/useKeyboardShortcuts'
import { ResizablePanel, ResizableHandle } from './ui/resizable'
import { Dialog, DialogContent, DialogHeader, DialogTitle, DialogDescription } from './ui/dialog'
import { useState, useEffect } from 'react'
import { Sun, Moon, Menu, X, HelpCircle, Search, ChevronLeft, ChevronRight, PanelRightOpen } from 'lucide-react'
import { Input } from './ui/input'

interface NavSection {
  title: string
  items: { path: string; label: string; shortcut?: string }[]
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
      { path: '/chat', label: 'Chat', shortcut: 'g+c' },
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
      { path: '/roles', label: 'Role Templates' },
      { path: '/delegation', label: 'Delegation' },
      { path: '/eval', label: 'Eval' },
    ],
  },
]

export function Layout() {
  const location = useLocation()
  const navigate = useNavigate()
  const { theme, setTheme, sidebar, inspector, setSidebarOpen, setInspectorOpen, setSidebarWidth, setInspectorWidth } = useUIStore()
  const [isMobile, setIsMobile] = useState(false)
  const [showMobileNav, setShowMobileNav] = useState(false)
  const [showMobileInspector, setShowMobileInspector] = useState(false)
  const [helpOpen, setHelpOpen] = useState(false)

  // Check mobile viewport
  useEffect(() => {
    const checkMobile = () => {
      setIsMobile(window.innerWidth < 1024)
    }
    checkMobile()
    window.addEventListener('resize', checkMobile)
    return () => window.removeEventListener('resize', checkMobile)
  }, [])

  // Close mobile drawers with Escape
  useEffect(() => {
    if (!isMobile || (!showMobileNav && !showMobileInspector)) {
      return
    }

    const handleKeyDown = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        setShowMobileNav(false)
        setShowMobileInspector(false)
      }
    }

    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [isMobile, showMobileNav, showMobileInspector])

  // Calculate effective theme
  const effectiveTheme = theme === 'system'
    ? (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
    : theme

  // Keyboard shortcuts
  useKeyboardShortcuts({
    'f1': () => setHelpOpen(true),
    '?': () => setHelpOpen(true),
    '/': () => {
      const searchInput = document.querySelector('[data-search-input]') as HTMLInputElement
      if (searchInput) {
        searchInput.focus()
      }
    },
    'g+c': () => navigate('/chat'),
  })

  const toggleTheme = () => {
    const newTheme = effectiveTheme === 'dark' ? 'light' : 'dark'
    setTheme(newTheme)
  }

  const handleSidebarResize = (width: number) => {
    setSidebarWidth(width)
  }

  const handleInspectorResize = (width: number) => {
    setInspectorWidth(width)
  }

  return (
    <div className="min-h-screen flex flex-col bg-background">
      {/* Header */}
      <header className="border-b bg-card px-4 py-3 flex items-center justify-between shrink-0">
        <div className="flex items-center gap-3 min-w-0">
          {isMobile && (
            <>
              <Button
                variant="ghost"
                size="icon"
                onClick={() => setShowMobileNav(true)}
                className="lg:hidden shrink-0"
                aria-label="Open navigation menu"
              >
                <Menu className="h-5 w-5" />
              </Button>
              <Button
                variant="secondary"
                size="sm"
                onClick={() => setShowMobileInspector(true)}
                className="lg:hidden shrink-0 gap-1 px-2"
                aria-label="Open inspector drawer"
              >
                <PanelRightOpen className="h-4 w-4" />
                <span className="text-xs font-medium">Inspector</span>
              </Button>
            </>
          )}
          <h1 className="text-lg font-semibold truncate">Openclawssy Dashboard</h1>
          <span className="text-xs text-muted-foreground bg-muted px-2 py-1 rounded">
            React
          </span>
          <span className="text-xs text-muted-foreground bg-muted px-2 py-1 rounded hidden sm:inline">
            Runtime Active
          </span>
        </div>
        <div className="flex items-center gap-2">
          <div className="relative hidden sm:block">
            <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
            <Input
              data-search-input
              type="text"
              placeholder="Search... (/ to focus)"
              className="pl-9 w-64"
            />
          </div>
          <Button
            variant="ghost"
            size="icon"
            onClick={() => setHelpOpen(true)}
            title="Help (F1 or ?)"
          >
            <HelpCircle className="h-5 w-5" />
          </Button>
          <Button
            variant="ghost"
            size="icon"
            onClick={toggleTheme}
            title={effectiveTheme === 'dark' ? 'Switch to light' : 'Switch to dark'}
          >
            {effectiveTheme === 'dark' ? <Sun className="h-5 w-5" /> : <Moon className="h-5 w-5" />}
          </Button>
        </div>
      </header>

      {/* Main Content Area */}
      <div className="flex-1 flex overflow-hidden">
        {/* Sidebar Navigation - Desktop */}
        {!isMobile && sidebar.isOpen && (
          <>
            <ResizablePanel
              defaultWidth={sidebar.width}
              minWidth={180}
              maxWidth={400}
              onResize={handleSidebarResize}
              direction="right"
              className="border-r bg-card"
            >
              <nav className="p-4 flex flex-col gap-4 h-full overflow-y-auto">
                <Button
                  variant="ghost"
                  size="sm"
                  className="self-end mb-2"
                  onClick={() => setSidebarOpen(false)}
                >
                  <ChevronLeft className="h-4 w-4 mr-1" />
                  Collapse
                </Button>
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
                            className={`px-3 py-2 rounded-md text-sm transition-colors flex items-center justify-between ${
                              isActive
                                ? 'bg-primary text-primary-foreground font-medium'
                                : 'hover:bg-muted text-muted-foreground hover:text-foreground'
                            }`}
                          >
                            <span>{item.label}</span>
                            {item.shortcut && (
                              <kbd className="text-xs bg-muted px-1.5 py-0.5 rounded opacity-60">
                                {item.shortcut}
                              </kbd>
                            )}
                          </Link>
                        )
                      })}
                    </div>
                  </div>
                ))}
              </nav>
            </ResizablePanel>
            <ResizableHandle direction="horizontal" />
          </>
        )}

        {/* Collapsed Sidebar Toggle */}
        {!isMobile && !sidebar.isOpen && (
          <div className="border-r bg-card p-2 flex flex-col items-center gap-2 shrink-0">
            <Button
              variant="ghost"
              size="icon"
              onClick={() => setSidebarOpen(true)}
              title="Expand sidebar"
            >
              <ChevronRight className="h-4 w-4" />
            </Button>
            <div className="w-full h-px bg-border my-1" />
            {navSections.map((section) => (
              <div key={section.title} className="flex flex-col gap-1">
                {section.items.slice(0, 3).map((item) => {
                  const isActive = location.pathname === item.path ||
                    (item.path !== '/' && location.pathname.startsWith(item.path))
                  return (
                    <Link
                      key={item.path}
                      to={item.path}
                      className={`w-8 h-8 flex items-center justify-center rounded-md text-xs font-medium transition-colors ${
                        isActive
                          ? 'bg-primary text-primary-foreground'
                          : 'hover:bg-muted text-muted-foreground hover:text-foreground'
                      }`}
                      title={item.label}
                    >
                      {item.label.charAt(0)}
                    </Link>
                  )
                })}
              </div>
            ))}
          </div>
        )}

        {/* Main Area */}
        <main className="flex-1 overflow-auto">
          <Outlet />
        </main>

        {/* Inspector Panel - Desktop */}
        {!isMobile && inspector.isOpen && (
          <>
            <ResizableHandle direction="horizontal" />
            <ResizablePanel
              defaultWidth={inspector.width}
              minWidth={200}
              maxWidth={500}
              onResize={handleInspectorResize}
              direction="left"
              className="border-l bg-card"
            >
              <div className="p-4 h-full overflow-y-auto">
                <div className="flex items-center justify-between mb-4">
                  <h3 className="text-sm font-medium">Inspector</h3>
                  <Button
                    variant="ghost"
                    size="icon"
                    className="h-6 w-6"
                    onClick={() => setInspectorOpen(false)}
                  >
                    <X className="h-4 w-4" />
                  </Button>
                </div>
                <p className="text-xs text-muted-foreground">
                  Select an item to view details
                </p>
              </div>
            </ResizablePanel>
          </>
        )}

        {/* Collapsed Inspector Toggle */}
        {!isMobile && !inspector.isOpen && (
          <div className="border-l bg-card p-2 flex items-center shrink-0">
            <Button
              variant="ghost"
              size="icon"
              onClick={() => setInspectorOpen(true)}
              title="Expand inspector"
            >
              <ChevronLeft className="h-4 w-4" />
            </Button>
          </div>
        )}
      </div>

      {/* Footer */}
      <footer className="border-t bg-card px-4 py-2 text-xs text-muted-foreground flex items-center justify-between shrink-0">
        <span>Dashboard active</span>
        <div className="flex items-center gap-4">
          <span className="hidden sm:inline">Press ? for keyboard shortcuts</span>
          <span>18 routes configured</span>
        </div>
      </footer>

      {/* Mobile Navigation Drawer */}
      {isMobile && showMobileNav && (
        <div className="fixed inset-0 z-50">
          <div className="absolute inset-0 bg-black/50" onClick={() => setShowMobileNav(false)} />
          <div className="absolute left-0 top-0 bottom-0 w-72 bg-card border-r p-4 overflow-y-auto">
            <div className="flex items-center justify-between mb-4">
              <h2 className="font-semibold">Navigation</h2>
              <Button variant="ghost" size="icon" aria-label="Close navigation drawer" onClick={() => setShowMobileNav(false)}>
                <X className="h-5 w-5" />
              </Button>
            </div>
            {navSections.map((section) => (
              <div key={section.title} className="mb-4">
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
                        onClick={() => setShowMobileNav(false)}
                        className={`px-3 py-2 rounded-md text-sm transition-colors flex items-center justify-between ${
                          isActive
                            ? 'bg-primary text-primary-foreground font-medium'
                            : 'hover:bg-muted text-muted-foreground hover:text-foreground'
                        }`}
                      >
                        <span>{item.label}</span>
                        {item.shortcut && (
                          <kbd className="text-xs bg-muted px-1.5 py-0.5 rounded opacity-60">
                            {item.shortcut}
                          </kbd>
                        )}
                      </Link>
                    )
                  })}
                </div>
              </div>
            ))}
          </div>
        </div>
      )}

      {/* Mobile Inspector Drawer */}
      {isMobile && showMobileInspector && (
        <div className="fixed inset-0 z-50">
          <div className="absolute inset-0 bg-black/50" onClick={() => setShowMobileInspector(false)} />
          <div className="absolute right-0 top-0 bottom-0 w-72 bg-card border-l p-4 overflow-y-auto">
            <div className="flex items-center justify-between mb-4">
              <h2 className="font-semibold">Inspector</h2>
              <Button
                variant="ghost"
                size="sm"
                className="gap-1"
                aria-label="Close inspector drawer"
                onClick={() => setShowMobileInspector(false)}
              >
                <X className="h-4 w-4" />
                <span className="text-xs">Close</span>
              </Button>
            </div>
            <p className="text-xs text-muted-foreground">
              Select an item to view details
            </p>
          </div>
        </div>
      )}

      {/* Help Dialog */}
      <Dialog open={helpOpen} onOpenChange={setHelpOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>Keyboard Shortcuts</DialogTitle>
            <DialogDescription>
              Navigate the dashboard quickly with these shortcuts
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            <div className="grid grid-cols-2 gap-2 text-sm">
              <div className="flex items-center justify-between">
                <kbd className="bg-muted px-2 py-1 rounded font-mono">F1</kbd>
                <span className="text-muted-foreground">Open help</span>
              </div>
              <div className="flex items-center justify-between">
                <kbd className="bg-muted px-2 py-1 rounded font-mono">?</kbd>
                <span className="text-muted-foreground">Open help (alt)</span>
              </div>
              <div className="flex items-center justify-between">
                <kbd className="bg-muted px-2 py-1 rounded font-mono">/</kbd>
                <span className="text-muted-foreground">Focus search</span>
              </div>
              <div className="flex items-center justify-between">
                <kbd className="bg-muted px-2 py-1 rounded font-mono">g c</kbd>
                <span className="text-muted-foreground">Go to chat</span>
              </div>
            </div>
            <div className="border-t pt-4">
              <h4 className="font-medium mb-2">Navigation Tips</h4>
              <ul className="text-sm text-muted-foreground space-y-1 list-disc list-inside">
                <li>Resize panels by dragging the handle between them</li>
                <li>Theme preference is saved to localStorage</li>
                <li>Use mobile drawer on narrow screens</li>
              </ul>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
