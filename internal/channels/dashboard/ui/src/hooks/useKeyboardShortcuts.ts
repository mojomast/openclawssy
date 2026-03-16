import { useEffect, useCallback, useMemo, useRef } from 'react'

interface ShortcutMap {
  [key: string]: () => void
}

interface ParsedShortcut {
  keys: string[]
  hasModifier: boolean
  hasShift: boolean
  hasAlt: boolean
  handler: () => void
}

interface ActiveSequence {
  shortcut: ParsedShortcut
  nextIndex: number
  expiresAt: number
}

const SEQUENCE_TIMEOUT_MS = 750

function isEditableTarget(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) {
    return false
  }

  if (target.isContentEditable) {
    return true
  }

  return Boolean(
    target.closest(
      'input, textarea, select, [contenteditable=""], [contenteditable="true"], [contenteditable="plaintext-only"]'
    )
  )
}

function shouldIgnoreShortcutForTyping(event: KeyboardEvent): boolean {
  if (event.metaKey || event.ctrlKey || event.altKey) {
    return false
  }

  return isEditableTarget(event.target) || isEditableTarget(document.activeElement)
}

export function useKeyboardShortcuts(shortcuts: ShortcutMap) {
  const activeSequenceRef = useRef<ActiveSequence | null>(null)

  const parsedShortcuts = useMemo<ParsedShortcut[]>(() => {
    return Object.entries(shortcuts)
      .map(([shortcut, handler]) => {
        const parts = shortcut.toLowerCase().split('+')
        const hasModifier = parts.includes('cmd') || parts.includes('ctrl')
        const hasShift = parts.includes('shift')
        const hasAlt = parts.includes('alt')
        const keys = parts.filter(
          (part) => !['cmd', 'ctrl', 'shift', 'alt'].includes(part)
        )

        return {
          keys,
          hasModifier,
          hasShift,
          hasAlt,
          handler,
        }
      })
      .filter((shortcut) => shortcut.keys.length > 0)
  }, [shortcuts])

  const handleKeyDown = useCallback(
    (event: KeyboardEvent) => {
      const now = Date.now()
      const key = event.key.toLowerCase()
      const isModifierPressed = event.metaKey || event.ctrlKey

      if (shouldIgnoreShortcutForTyping(event)) {
        activeSequenceRef.current = null
        return
      }

      const modifiersMatch = (shortcut: ParsedShortcut) =>
        shortcut.hasModifier === isModifierPressed &&
        shortcut.hasShift === event.shiftKey &&
        shortcut.hasAlt === event.altKey

      const activeSequence = activeSequenceRef.current
      if (activeSequence && activeSequence.expiresAt <= now) {
        activeSequenceRef.current = null
      }

      const pendingSequence = activeSequenceRef.current
      if (pendingSequence) {
        const expectedKey = pendingSequence.shortcut.keys[pendingSequence.nextIndex]
        if (modifiersMatch(pendingSequence.shortcut) && key === expectedKey) {
          if (pendingSequence.nextIndex === pendingSequence.shortcut.keys.length - 1) {
            event.preventDefault()
            activeSequenceRef.current = null
            pendingSequence.shortcut.handler()
            return
          }

          activeSequenceRef.current = {
            shortcut: pendingSequence.shortcut,
            nextIndex: pendingSequence.nextIndex + 1,
            expiresAt: now + SEQUENCE_TIMEOUT_MS,
          }
          return
        }

        activeSequenceRef.current = null
      }

      for (const shortcut of parsedShortcuts) {
        if (shortcut.keys.length !== 1) {
          continue
        }

        if (modifiersMatch(shortcut) && key === shortcut.keys[0]) {
          event.preventDefault()
          shortcut.handler()
          return
        }
      }

      for (const shortcut of parsedShortcuts) {
        if (shortcut.keys.length <= 1) {
          continue
        }

        if (modifiersMatch(shortcut) && key === shortcut.keys[0]) {
          activeSequenceRef.current = {
            shortcut,
            nextIndex: 1,
            expiresAt: now + SEQUENCE_TIMEOUT_MS,
          }
          return
        }
      }
    },
    [parsedShortcuts]
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
