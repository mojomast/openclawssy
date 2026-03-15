/**
 * Authentication store - manages bearer token in localStorage
 */
import { create } from 'zustand'
import { persist } from 'zustand/middleware'

interface AuthState {
  // State
  token: string | null
  isAuthenticated: boolean

  // Actions
  setToken: (token: string) => void
  clearToken: () => void
  promptForToken: () => string | null
}

export const useAuthStore = create<AuthState>()(
  persist(
    (set, get) => ({
      // Initial state
      token: null,
      isAuthenticated: false,

      // Set token from external source (e.g., after prompt)
      setToken: (token: string) => {
        if (token) {
          localStorage.setItem('openclawssy.dashboard.bearer', token)
          set({ token, isAuthenticated: true })
        }
      },

      // Clear token (logout)
      clearToken: () => {
        localStorage.removeItem('openclawssy.dashboard.bearer')
        set({ token: null, isAuthenticated: false })
      },

      // Prompt user for token
      promptForToken: () => {
        const prompted = window.prompt('Enter dashboard bearer token')?.trim()
        if (prompted) {
          get().setToken(prompted)
          return prompted
        }
        return null
      },
    }),
    {
      name: 'auth-store',
      partialize: (state) => ({ token: state.token }),
      onRehydrateStorage: () => (state) => {
        // Update isAuthenticated based on token after rehydration
        if (state && state.token) {
          state.isAuthenticated = true
        }
      },
    }
  )
)

// Initialize token from localStorage on load
const storedToken = localStorage.getItem('openclawssy.dashboard.bearer')
if (storedToken) {
  useAuthStore.setState({ token: storedToken, isAuthenticated: true })
}
