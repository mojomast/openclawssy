/**
 * Agent store - manages agent list and selection
 */
import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'
import { api } from '../lib/api'

export interface Agent {
  id: string
  name: string
  description?: string
  enabled: boolean
  model?: string
  skills: string[]
}

interface AgentState {
  // State
  agents: Agent[]
  selectedAgentId: string | null
  isLoading: boolean
  error: string | null

  // Actions
  fetchAgents: () => Promise<void>
  selectAgent: (agentId: string) => void
  clearSelection: () => void
  refreshAgents: () => Promise<void>
}

export const useAgentStore = create<AgentState>()(
  persist(
    (set, get) => ({
      // Initial state
      agents: [],
      selectedAgentId: null,
      isLoading: false,
      error: null,

      // Fetch agents from API
      fetchAgents: async () => {
        set({ isLoading: true, error: null })
        try {
          // Try to fetch from API
          const response = await api.get<{ agents: Agent[] }>('/api/admin/agents')
          const agents = response.agents || []
          
          set({
            agents,
            isLoading: false,
            // Auto-select first agent if none selected
            selectedAgentId: get().selectedAgentId || (agents[0]?.id ?? null),
          })
        } catch (err) {
          // Fallback to default agent if API not available
          const defaultAgent: Agent = {
            id: 'default',
            name: 'Default Agent',
            description: 'The default agent profile',
            enabled: true,
            skills: [],
          }
          set({
            agents: [defaultAgent],
            selectedAgentId: 'default',
            isLoading: false,
            error: null,
          })
        }
      },

      // Select an agent
      selectAgent: (agentId: string) => {
        const { agents } = get()
        const agentExists = agents.some((a) => a.id === agentId)
        if (agentExists) {
          set({ selectedAgentId: agentId })
        }
      },

      // Clear agent selection
      clearSelection: () => set({ selectedAgentId: null }),

      // Refresh agents (force refetch)
      refreshAgents: async () => {
        await get().fetchAgents()
      },
    }),
    {
      name: 'agent-store',
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({
        selectedAgentId: state.selectedAgentId,
      }),
    }
  )
)
