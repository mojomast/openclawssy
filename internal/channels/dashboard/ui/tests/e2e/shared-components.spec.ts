import { expect, test } from '@playwright/test'

test.describe('Shared Components', () => {
  test.beforeEach(async ({ page }) => {
    await page.addInitScript(() => {
      window.localStorage.setItem('openclawssy.dashboard.bearer', 'e2e-token')
      window.localStorage.setItem(
        'ui-store',
        JSON.stringify({
          state: {
            theme: 'system',
            sidebar: { isOpen: true, width: 240, collapsedSections: [] },
            inspector: { isOpen: true, width: 320 },
          },
          version: 0,
        })
      )
    })

    await page.route('**/api/admin/status', async (route) => {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          model: { provider: 'openai', name: 'gpt-4.1-mini' },
          run_count: 42,
        }),
      })
    })

    await page.route('**/api/admin/control-plane/features', async (route) => {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          features: {
            instance_control: true,
            instance_agents: true,
            wizard: true,
            eval: true,
          },
        }),
      })
    })

    await page.route('**/api/admin/instances/active', async (route) => {
      return route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          instance: {
            id: 'alpha',
            name: 'Alpha',
          },
        }),
      })
    })

    await page.goto('/dashboard#/help')
  })

  test('Layout shell renders with header, nav, main, inspector panels', async ({ page }) => {
    await expect(page.getByRole('heading', { name: 'Openclawssy Dashboard', exact: true })).toBeVisible()
    await expect(page.getByText('React')).toBeVisible()
    await expect(page.getByText('Runtime Active')).toBeVisible()
    await expect(page.getByTestId('header-active-instance')).toContainText('Instance: Alpha (alpha)')

    await expect(page.getByRole('heading', { name: 'Dashboard', exact: true })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Operations', exact: true })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Control Plane', exact: true })).toBeVisible()

    await expect(page.locator('.border-l.bg-card').getByRole('heading', { name: 'Inspector', exact: true })).toBeVisible()

    await expect(page.locator('footer').getByText('Dashboard active', { exact: true })).toBeVisible()
    await expect(page.locator('footer').getByText('20 routes configured', { exact: true })).toBeVisible()
  })

  test('Nav sidebar shows links for all routes', async ({ page }) => {
    await expect(page.getByRole('link', { name: 'Help', exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Workspace', exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Secrets', exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: /^Chat/ })).toBeVisible()

    await expect(page.getByRole('link', { name: 'Runs', exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Sessions', exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Scheduler', exact: true })).toBeVisible()

    await expect(page.getByRole('link', { name: 'Agent Contract', exact: true })).toBeVisible()
    await expect(page.getByRole('link', { name: 'Prompt Stack', exact: true })).toBeVisible()
  })

  test('Theme toggle switches dark/light and persists', async ({ page }) => {
    const themeButton = page.getByRole('button', { name: /Switch to (light|dark)/ })
    await expect(themeButton).toBeVisible()
    await themeButton.click()

    const theme = await page.evaluate(() => {
      const store = localStorage.getItem('ui-store')
      return store ? JSON.parse(store).state?.theme : null
    })

    expect(['light', 'dark']).toContain(theme)
  })

  test('Help dialog opens with F1 key', async ({ page }) => {
    await page.keyboard.press('F1')
    await expect(page.getByRole('dialog')).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Keyboard Shortcuts' })).toBeVisible()
    await expect(page.getByRole('dialog').getByText('F1', { exact: true })).toBeVisible()
    await expect(page.getByRole('dialog').getByText('Open help', { exact: true })).toBeVisible()
  })

  test('Help dialog opens with ? key', async ({ page }) => {
    await page.keyboard.press('?')
    await expect(page.getByRole('dialog')).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Keyboard Shortcuts' })).toBeVisible()
  })

  test('Search input focuses with / key', async ({ page }) => {
    const searchInput = page.locator('[data-search-input]')
    await page.keyboard.press('/')
    await expect(searchInput).toBeFocused()
  })

  test('g alone does not navigate', async ({ page }) => {
    await page.keyboard.press('g')

    await expect(page).toHaveURL(/.*\/help/)
    await expect(page.getByRole('heading', { name: 'Help Center', exact: true })).toBeVisible()
  })

  test('g then c outside timeout does not navigate', async ({ page }) => {
    await page.keyboard.press('g')
    await page.waitForTimeout(900)
    await page.keyboard.press('c')

    await expect(page).toHaveURL(/.*\/help/)
    await expect(page.getByRole('heading', { name: 'Help Center', exact: true })).toBeVisible()
  })

  test('g+c keyboard shortcut navigates to chat', async ({ page }) => {
    await page.keyboard.press('g')
    await page.keyboard.press('c')

    await expect(page).toHaveURL(/.*\/chat/)
    await expect(page.getByRole('heading', { name: 'Chat' })).toBeVisible()
  })

  test('Sidebar can be collapsed and expanded', async ({ page }) => {
    await page.getByRole('button', { name: 'Collapse' }).click()

    const expandButton = page.getByRole('button', { name: 'Expand sidebar', exact: true })
    await expect(expandButton).toBeVisible()
    await expandButton.click()

    await expect(page.getByRole('heading', { name: 'Dashboard', exact: true })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Operations', exact: true })).toBeVisible()
    await expect(page.getByRole('heading', { name: 'Control Plane', exact: true })).toBeVisible()
  })

  test('Inspector panel can be closed and opened', async ({ page }) => {
    const inspector = page.locator('aside, .border-l.bg-card').filter({ hasText: 'Inspector' })
    const closeButton = inspector.getByRole('button').first()
    await closeButton.click()

    const expandButton = page.getByRole('button', { name: 'Expand inspector', exact: true })
    await expect(expandButton).toBeVisible()
    await expandButton.click()

    await expect(page.locator('.border-l.bg-card').getByRole('heading', { name: 'Inspector', exact: true })).toBeVisible()
  })

  test('Footer displays React-only status and route count', async ({ page }) => {
    await expect(page.locator('footer').getByText('Dashboard active', { exact: true })).toBeVisible()
    await expect(page.locator('footer').getByText('20 routes configured', { exact: true })).toBeVisible()
    await expect(page.locator('footer').getByText('Press ? for keyboard shortcuts', { exact: true })).toBeVisible()
  })

  test('Navigation links work and show active state', async ({ page }) => {
    await page.getByRole('link', { name: 'Secrets', exact: true }).click()
    await expect(page).toHaveURL(/.*\/secrets/)
  })

  test('Mobile navigation drawer appears on narrow screens', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 })

    await page.getByRole('button', { name: 'Open navigation menu', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Navigation' })).toBeVisible()

    await page.getByRole('button', { name: 'Close navigation drawer', exact: true }).click()
  })

  test('Mobile inspector trigger is visible in header and close control dismisses drawer', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 })

    await expect(page.locator('header').getByText('Inspector', { exact: true })).toBeVisible()
    await page.getByRole('button', { name: 'Open inspector drawer', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Inspector' })).toBeVisible()

    await page.getByRole('button', { name: 'Close inspector drawer', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Inspector' })).toBeHidden()
  })

  test('Mobile inspector drawer can be opened and closed via backdrop', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 })

    await page.getByRole('button', { name: 'Open inspector drawer', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Inspector' })).toBeVisible()

    await page.mouse.click(20, 20)
    await expect(page.getByRole('heading', { name: 'Inspector' })).toBeHidden()
  })

  test('Mobile inspector drawer closes on Escape', async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 667 })

    await page.getByRole('button', { name: 'Open inspector drawer', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Inspector' })).toBeVisible()

    await page.keyboard.press('Escape')
    await expect(page.getByRole('heading', { name: 'Inspector' })).toBeHidden()
  })
})
