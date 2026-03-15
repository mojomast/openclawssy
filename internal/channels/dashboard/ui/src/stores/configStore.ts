/**
 * Config store - manages application configuration state and CRUD operations
 */
import { create } from 'zustand'
import { createJSONStorage, persist } from 'zustand/middleware'
import { api } from '../lib/api'

// Config types based on backend schema
export interface Config {
  bind_address: string
  port: number
  workspace: string
  thinking: boolean
  model_provider: string
  model: string
  temperature: number
  max_tokens: number
  timeout_seconds: number
  agents: AgentConfig
  memory: MemoryConfig
  sandbox: SandboxConfig
  scheduler: SchedulerConfig
  capabilities: Record<string, boolean>
}

export interface AgentConfig {
  profiles: Record<string, AgentProfile>
  subagent_defaults: SubagentDefaults
}

export interface AgentProfile {
  enabled: boolean
  model_override?: string
  self_improvement: boolean
}

export interface SubagentDefaults {
  thinking: boolean
  delegation_mode: string
  max_iterations: number
  timeout_seconds: number
  allowed_tools: string[]
}

export interface MemoryConfig {
  enabled: boolean
  max_working_items: number
  max_prompt_tokens: number
  auto_checkpoint: boolean
  proactive_consolidation: boolean
  embeddings: boolean
}

export interface SandboxConfig {
  active: boolean
  provider: string
  docker?: DockerConfig
}

export interface DockerConfig {
  image: string
  workspace_mount: string
}

export interface SchedulerConfig {
  catch_up: boolean
  max_concurrent: number
}

interface ConfigState {
  // State
  config: Config | null
  isLoading: boolean
  error: string | null
  hasUnsavedChanges: boolean
  pendingChanges: Partial<Config> | null

  // Actions
  fetchConfig: () => Promise<void>
  updateConfig: (updates: Partial<Config>) => void
  saveConfig: () => Promise<void>
  reloadConfig: () => Promise<void>
  clearError: () => void
  discardChanges: () => void
}

const defaultConfig: Config = {
  bind_address: '127.0.0.1',
  port: 8080,
  workspace: '/workspace',
  thinking: false,
  model_provider: 'openai',
  model: 'gpt-4',
  temperature: 0.7,
  max_tokens: 4096,
  timeout_seconds: 120,
  agents: {
    profiles: {},
    subagent_defaults: {
      thinking: false,
      delegation_mode: 'none',
      max_iterations: 50,
      timeout_seconds: 60,
      allowed_tools: [],
    },
  },
  memory: {
    enabled: true,
    max_working_items: 100,
    max_prompt_tokens: 4000,
    auto_checkpoint: true,
    proactive_consolidation: false,
    embeddings: false,
  },
  sandbox: {
    active: false,
    provider: 'docker',
    docker: {
      image: 'openclawssy/sandbox:latest',
      workspace_mount: '/workspace',
    },
  },
  scheduler: {
    catch_up: true,
    max_concurrent: 5,
  },
  capabilities: {},
}

export const useConfigStore = create<ConfigState>()(
  persist(
    (set, get) => ({
      // Initial state
      config: null,
      isLoading: false,
      error: null,
      hasUnsavedChanges: false,
      pendingChanges: null,

      // Fetch config from API
      fetchConfig: async () => {
        set({ isLoading: true, error: null })
        try {
          const response = await api.get<{ config: Config }>('/api/admin/config')
          set({
            config: response.config,
            isLoading: false,
            hasUnsavedChanges: false,
            pendingChanges: null,
          })
        } catch (err) {
          set({
            error: err instanceof Error ? err.message : 'Failed to fetch config',
            isLoading: false,
          })
        }
      },

      // Stage config changes (does not save)
      updateConfig: (updates: Partial<Config>) => {
        const { config, pendingChanges } = get()
        const current = pendingChanges || config || defaultConfig
        set({
          pendingChanges: { ...current, ...updates },
          hasUnsavedChanges: true,
        })
      },

      // Save pending changes to API
      saveConfig: async () => {
        const { pendingChanges, config } = get()
        if (!pendingChanges) return

        set({ isLoading: true, error: null })
        try {
          const newConfig = { ...config, ...pendingChanges } as Config
          await api.patch('/api/admin/config', newConfig)
          set({
            config: newConfig,
            pendingChanges: null,
            hasUnsavedChanges: false,
            isLoading: false,
          })
        } catch (err) {
          set({
            error: err instanceof Error ? err.message : 'Failed to save config',
            isLoading: false,
          })
        }
      },

      // Reload config from API (discard changes)
      reloadConfig: async () => {
        set({ pendingChanges: null, hasUnsavedChanges: false })
        await get().fetchConfig()
      },

      // Clear error state
      clearError: () => set({ error: null }),

      // Discard pending changes
      discardChanges: () => {
        set({
          pendingChanges: null,
          hasUnsavedChanges: false,
        })
      },
    }),
    {
      name: 'config-store',
      storage: createJSONStorage(() => localStorage),
      partialize: (state) => ({
        config: state.config,
        hasUnsavedChanges: state.hasUnsavedChanges,
        pendingChanges: state.pendingChanges,
      }),
    }
  )
)
