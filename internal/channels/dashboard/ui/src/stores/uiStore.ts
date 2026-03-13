/**
 * UI store - manages theme, sidebar state, inspector state, and other UI preferences
 */
import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'

export type Theme = 'light' | 'dark' | 'system'

interface InspectorState {
  isOpen: boolean
  width: number
}

interface SidebarState {
  isOpen: boolean
  width: number
  collapsedSections: string[]
}

export interface Toast {
  id: string
  type: 'success' | 'error' | 'warning' | 'info'
  title: string
  message?: string
  duration?: number
}

interface UIState {
  // Theme
  theme: Theme
  setTheme: (theme: Theme) => void
  toggleTheme: () => void

  // Sidebar
  sidebar: SidebarState
  toggleSidebar: () => void
  setSidebarOpen: (isOpen: boolean) => void
  toggleSection: (section: string) => void

  // Inspector panel
  inspector: InspectorState
  toggleInspector: () => void
  setInspectorOpen: (isOpen: boolean) => void
  setInspectorWidth: (width: number) => void

  // Search
  searchQuery: string
  setSearchQuery: (query: string) => void
  clearSearch: () => void

  // Notifications
  toasts: Toast[]
  addToast: (toast: Omit<Toast, 'id'>) => void
  removeToast: (id: string) => void
  clearToasts: () => void

  // Modals/Drawers
  activeModal: string | null
  modalData: unknown
  openModal: (modal: string, data?: unknown) => void
  closeModal: () => void
}

// Generate unique ID for toasts
let toastId = 0
const generateToastId = () => `toast-${++toastId}`

export const useUIStore = create<UIState>()(
  persist(
    (set, get) => ({
      // Initial state
      theme: 'system',
      sidebar: {
        isOpen: true,
        width: 240,
        collapsedSections: [],
      },
      inspector: {
        isOpen: true,
        width: 320,
      },
      searchQuery: '',
      toasts: [],
      activeModal: null,
      modalData: null,

      // Theme actions
      setTheme: (theme: Theme) => {
        set({ theme })
        // Apply theme to document
        const effectiveTheme = theme === 'system'
          ? (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
          : theme
        document.documentElement.setAttribute('data-theme', effectiveTheme)
      },

      toggleTheme: () => {
        const current = get().theme
        const newTheme: Theme = current === 'light' ? 'dark' : current === 'dark' ? 'system' : 'light'
        get().setTheme(newTheme)
      },

      // Sidebar actions
      toggleSidebar: () => {
        set((state) => ({
          sidebar: { ...state.sidebar, isOpen: !state.sidebar.isOpen },
        }))
      },

      setSidebarOpen: (isOpen: boolean) => {
        set((state) => ({
          sidebar: { ...state.sidebar, isOpen },
        }))
      },

      toggleSection: (section: string) => {
        set((state) => {
          const { collapsedSections } = state.sidebar
          const isCollapsed = collapsedSections.includes(section)
          return {
            sidebar: {
              ...state.sidebar,
              collapsedSections: isCollapsed
                ? collapsedSections.filter((s) => s !== section)
                : [...collapsedSections, section],
            },
          }
        })
      },

      // Inspector actions
      toggleInspector: () => {
        set((state) => ({
          inspector: { ...state.inspector, isOpen: !state.inspector.isOpen },
        }))
      },

      setInspectorOpen: (isOpen: boolean) => {
        set((state) => ({
          inspector: { ...state.inspector, isOpen },
        }))
      },

      setInspectorWidth: (width: number) => {
        set((state) => ({
          inspector: { ...state.inspector, width: Math.max(200, Math.min(600, width)) },
        }))
      },

      // Search actions
      setSearchQuery: (query: string) => set({ searchQuery: query }),
      clearSearch: () => set({ searchQuery: '' }),

      // Toast actions
      addToast: (toast: Omit<Toast, 'id'>) => {
        const id = generateToastId()
        set((state) => ({
          toasts: [...state.toasts, { ...toast, id }],
        }))
        // Auto-remove after duration
        if (toast.duration !== 0) {
          setTimeout(() => {
            get().removeToast(id)
          }, toast.duration || 5000)
        }
      },

      removeToast: (id: string) => {
        set((state) => ({
          toasts: state.toasts.filter((t) => t.id !== id),
        }))
      },

      clearToasts: () => set({ toasts: [] }),

      // Modal actions
      openModal: (modal: string, data?: unknown) => {
        set({ activeModal: modal, modalData: data })
      },

      closeModal: () => {
        set({ activeModal: null, modalData: null })
      },
    }),
    {
      name: 'ui-store',
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({
        theme: state.theme,
        sidebar: {
          isOpen: state.sidebar.isOpen,
          width: state.sidebar.width,
          collapsedSections: state.sidebar.collapsedSections,
        },
        inspector: {
          isOpen: state.inspector.isOpen,
          width: state.inspector.width,
        },
      }),
    }
  )
)

// Initialize theme on load
const storedTheme = localStorage.getItem('ui-store')
if (storedTheme) {
  try {
    const parsed = JSON.parse(storedTheme)
    const theme = parsed.state?.theme as Theme || 'system'
    const effectiveTheme = theme === 'system'
      ? (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light')
      : theme
    document.documentElement.setAttribute('data-theme', effectiveTheme)
  } catch {
    // Ignore parse errors
  }
}
