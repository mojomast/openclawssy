import { useEffect, useMemo, useRef } from "react"
import { useLocation } from "react-router-dom"
import { createApiClient } from "@/lib/api"
import { disposeSettingsPage, settingsPage, settingsPageHasUnsavedChanges } from "./settings.js"

const UNSAVED_SETTINGS_MESSAGE =
  "You have unsaved settings changes. Click OK to discard and navigate away, or Cancel to stay and save."

function routePathFromHref(href: string): string | null {
  try {
    const url = new URL(href, window.location.href)
    const hash = String(url.hash || "").replace(/^#/, "")
    if (!hash) {
      return null
    }
    const [rawPath] = hash.split("?")
    return rawPath.startsWith("/") ? rawPath : `/${rawPath}`
  } catch {
    return null
  }
}

function isSettingsRoute(path: string | null): boolean {
  return path === "/settings"
}

export function SettingsPage() {
  const containerRef = useRef<HTMLDivElement | null>(null)
  const location = useLocation()
  const apiClient = useMemo(() => createApiClient(), [])

  useEffect(() => {
    const container = containerRef.current
    if (!container) {
      return
    }
    void settingsPage.render({ container, apiClient })
  }, [apiClient, location.pathname, location.search])

  useEffect(() => {
    const handleLinkNavigation = (event: MouseEvent) => {
      if (!settingsPageHasUnsavedChanges()) {
        return
      }

      const target = event.target instanceof Element ? event.target.closest("a[href]") : null
      if (!target) {
        return
      }

      const href = target.getAttribute("href")
      if (!href) {
        return
      }

      const destinationPath = routePathFromHref(href)
      if (!destinationPath || isSettingsRoute(destinationPath)) {
        return
      }

      if (!window.confirm(UNSAVED_SETTINGS_MESSAGE)) {
        event.preventDefault()
        event.stopPropagation()
      }
    }

    const handleBeforeUnload = (event: BeforeUnloadEvent) => {
      if (!settingsPageHasUnsavedChanges()) {
        return
      }
      event.preventDefault()
      event.returnValue = ""
    }

    document.addEventListener("click", handleLinkNavigation, true)
    window.addEventListener("beforeunload", handleBeforeUnload)

    return () => {
      document.removeEventListener("click", handleLinkNavigation, true)
      window.removeEventListener("beforeunload", handleBeforeUnload)
      disposeSettingsPage()
    }
  }, [])

  return <div className="p-6" ref={containerRef} data-testid="settings-page" />
}
