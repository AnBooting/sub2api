import { expect, test, type Page } from '@playwright/test'

const harnessUrl = (caseName: string) => `/e2e/harness/index.html?case=${caseName}`

async function openDialog(page: Page, caseName = 'basic') {
  await page.goto(harnessUrl(caseName))
  await page.locator('#open').click()
  await expect(page.locator('#inner-first')).toBeVisible()
}

test('initial focus lands on the first enabled control', async ({ page }) => {
  await openDialog(page)
  await expect(page.locator('#inner-first')).toBeFocused()
})

test('Tab from the last control wraps to the first', async ({ page }) => {
  await openDialog(page)

  await page.locator('#inner-last').click()
  await expect(page.locator('#inner-last')).toBeFocused()

  await page.keyboard.press('Tab')
  await expect(page.locator('#inner-first')).toBeFocused()
})

test('Shift+Tab from the first control wraps to the last', async ({ page }) => {
  await openDialog(page)

  await page.locator('#inner-first').click()
  await page.keyboard.press('Shift+Tab')
  await expect(page.locator('#inner-last')).toBeFocused()
})

test('focus cannot escape to the page behind the dialog', async ({ page }) => {
  await openDialog(page)

  await page.locator('#inner-first').click()
  for (let i = 0; i < 6; i += 1) {
    await page.keyboard.press('Tab')
  }

  await expect(page.locator('#inner-first')).toBeFocused()
  await expect(page.locator('#open')).not.toBeFocused()
})

test('closing the dialog returns focus to the trigger', async ({ page }) => {
  await openDialog(page)

  await page.keyboard.press('Escape')
  await expect(page.locator('#inner-first')).toHaveCount(0)
  await expect(page.locator('#open')).toBeFocused()
})

test('Escape closes only the topmost nested dialog', async ({ page }) => {
  await openDialog(page, 'nested')

  await page.locator('#open-nested').click()
  await expect(page.locator('#nested-first')).toBeVisible()
  await expect(page.locator('#nested-first')).toBeFocused()

  await page.keyboard.press('Escape')
  await expect(page.locator('#nested-first')).toHaveCount(0)
  await expect(page.locator('#inner-first')).toBeVisible()

  await page.keyboard.press('Escape')
  await expect(page.locator('#inner-first')).toHaveCount(0)
})

test('backdrop click closes the dialog when enabled', async ({ page }) => {
  await openDialog(page, 'backdrop')

  await page.mouse.click(5, 5)
  await expect(page.locator('#inner-first')).toHaveCount(0)
})

test('backdrop click does not close the dialog by default', async ({ page }) => {
  await openDialog(page, 'basic')

  await page.mouse.click(5, 5)
  await expect(page.locator('#inner-first')).toBeVisible()
})

test('hidden ancestors and non-tabbable controls do not corrupt either boundary', async ({ page }) => {
  await openDialog(page, 'hidden-boundaries')
  await page.locator('#inner-last').focus()
  await page.keyboard.press('Tab')
  await expect(page.locator('#inner-first')).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(page.locator('#inner-last')).toBeFocused()
})

test('an empty dialog holds focus on its panel', async ({ page }) => {
  await page.goto(harnessUrl('empty'))
  await page.locator('#open').click()
  await expect(page.locator('.modal-content')).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(page.locator('.modal-content')).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(page.locator('.modal-content')).toBeFocused()
})

test('the real user API-key group portal belongs to the dialog Tab cycle', async ({ page }) => {
  await page.route('**/api/v1/**', async route => {
    const path = new URL(route.request().url()).pathname
    const group = { id: 1, name: 'Review group', platform: 'openai', status: 'active', subscription_type: 'standard', rate_multiplier: 1 }
    const data = path.endsWith('/admin/users/1/api-keys')
      ? { items: [{ id: 1, name: 'Review key', key: 'fixture-key-not-a-credential', status: 'active', group_id: 1, group, created_at: '2026-01-01T00:00:00Z' }], total: 1 }
      : path.endsWith('/admin/groups/all') ? [group] : {}
    await route.fulfill({ json: { code: 0, data } })
  })
  await page.goto(harnessUrl('user-keys'))
  await page.locator('#open').click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByText('Review key', { exact: true })).toBeVisible()
  await dialog.getByRole('button').filter({ hasText: 'Review group' }).click()
  const unbind = page.getByRole('button', { name: 'admin.users.none', exact: true })
  const groupOption = page.getByRole('button', { name: /Review group/ }).last()
  await expect(unbind).toBeVisible()
  // focus() keeps the real menu open; clicking this button would dismiss it.
  await dialog.getByRole('button', { name: 'keys.chain.edit', exact: true }).focus()
  await page.keyboard.press('Tab')
  await expect(unbind).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(groupOption).toBeFocused()
  await page.keyboard.press('Tab')
  await expect(dialog.getByRole('button', { name: 'Close dialog' })).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(groupOption).toBeFocused()
  await page.keyboard.press('Shift+Tab')
  await expect(unbind).toBeFocused()
})
