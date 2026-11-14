import { expect, test, type Page } from '@playwright/test'

const HARNESS = '/e2e/harness/date-range.html'
const INITIAL_RANGE = '2026-01-01..2026-01-02'

async function openPicker(page: Page) {
  await page.goto(HARNESS)
  await page.locator('.date-picker-trigger').click()
  await expect(page.locator('.date-picker-dropdown')).toBeVisible()
}

test('click opens the picker', async ({ page }) => {
  await openPicker(page)

  await expect(page.locator('.date-picker-preset').first()).toBeVisible()
})

test('clicking the trigger again closes the picker', async ({ page }) => {
  await openPicker(page)

  await page.locator('.date-picker-trigger').click()

  await expect(page.locator('.date-picker-dropdown')).toHaveCount(0)
})

test('Escape closes the picker', async ({ page }) => {
  await openPicker(page)

  await page.keyboard.press('Escape')

  await expect(page.locator('.date-picker-dropdown')).toHaveCount(0)
})

test('clicking outside closes the picker', async ({ page }) => {
  await openPicker(page)

  await page.locator('#outside').click()

  await expect(page.locator('.date-picker-dropdown')).toHaveCount(0)
})

test('choosing a preset and applying updates the range', async ({ page }) => {
  await openPicker(page)

  await expect(page.locator('#range-value')).toHaveText(INITIAL_RANGE)
  await page.locator('.date-picker-preset').first().click()
  await page.locator('.date-picker-apply').click()

  await expect(page.locator('.date-picker-dropdown')).toHaveCount(0)
  await expect(page.locator('#range-value')).not.toHaveText(INITIAL_RANGE)
})
