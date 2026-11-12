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
