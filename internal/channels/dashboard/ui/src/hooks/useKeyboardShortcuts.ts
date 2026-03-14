import { useEffect, useCallback } from 'react'

interface ShortcutMap {
  [key: string]: () => void
}

export function useKeyboardShortcuts(shortcuts: ShortcutMap) {
  const handleKeyDown = useCallback(
    (event: KeyboardEvent) => {
      const key = event.key.toLowerCase()
      const isModifierPressed = event.metaKey || event.ctrlKey

      for (const [shortcut, handler] of Object.entries(shortcuts)) {
        const parts = shortcut.toLowerCase().split('+')
        const hasModifier = parts.includes('cmd') || parts.includes('ctrl')
        const hasShift = parts.includes('shift')
        const hasAlt = parts.includes('alt')

        const mainKey = parts.find(
          (p) => !['cmd', 'ctrl', 'shift', 'alt'].includes(p)
        )

        if (!mainKey) continue

        const modifierMatch = hasModifier === isModifierPressed
        const shiftMatch = hasShift === event.shiftKey
        const altMatch = hasAlt === event.altKey
        const keyMatch = key === mainKey

        if (modifierMatch && shiftMatch && altMatch && keyMatch) {
          event.preventDefault()
          handler()
          return
        }
      }
    },
    [shortcuts]
  )

  useEffect(() => {
    window.addEventListener('keydown', handleKeyDown)
    return () => window.removeEventListener('keydown', handleKeyDown)
  }, [handleKeyDown])
}

// Navigation shortcuts (vi-style and common app patterns)
export const NAVIGATION_SHORTCUTS = {
  help: 'f1',
  helpAlt: '?',
  search: '/',
  goChat: 'g+c',
} as const
