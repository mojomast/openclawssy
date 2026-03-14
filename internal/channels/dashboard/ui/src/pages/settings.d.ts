export const settingsPage: {
  key: string
  title: string
  render(args: {
    container: HTMLElement
    apiClient: {
      get(path: string): Promise<unknown>
      post(path: string, body?: unknown): Promise<unknown>
      patch(path: string, body?: unknown): Promise<unknown>
      put(path: string, body?: unknown): Promise<unknown>
      delete(path: string): Promise<unknown>
    }
  }): Promise<void> | void
}

export function settingsPageHasUnsavedChanges(): boolean
export function disposeSettingsPage(): void
