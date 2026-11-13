import { expect, test, type Page } from '@playwright/test'

const HARNESS = '/e2e/harness/select.html'

async function openSelect(page: Page) {
  await page.goto(HARNESS)
  await page.locator('#harness-select').click()
  await expect(page.locator('.select-dropdown-portal')).toBeVisible()
}

test('click opens the dropdown and focuses the first enabled option', async ({ page }) => {
  await openSelect(page)

  await expect(page.locator('.select-option-focused')).toHaveText('Alpha')
})

test('ArrowDown opens the dropdown from a focused trigger', async ({ page }) => {
  await page.goto(HARNESS)

  await page.locator('#harness-select').focus()
  await page.keyboard.press('ArrowDown')

  await expect(page.locator('.select-dropdown-portal')).toBeVisible()
  await expect(page.locator('.select-option-focused')).toHaveText('Alpha')
})

test('arrow keys move the focused option, skip disabled ones and wrap around', async ({ page }) => {
  await openSelect(page)

  await page.keyboard.press('ArrowDown')
  await expect(page.locator('.select-option-focused')).toHaveText('Gamma')

  await page.keyboard.press('ArrowDown')
  await expect(page.locator('.select-option-focused')).toHaveText('Delta')

  await page.keyboard.press('ArrowDown')
  await expect(page.locator('.select-option-focused')).toHaveText('Alpha')

  await page.keyboard.press('ArrowUp')
  await expect(page.locator('.select-option-focused')).toHaveText('Delta')
})

test('Enter selects the focused option and returns focus to the trigger', async ({ page }) => {
  await openSelect(page)

  await page.keyboard.press('ArrowDown')
  await page.keyboard.press('Enter')

  await expect(page.locator('#selected-value')).toHaveText('gamma')
  await expect(page.locator('.select-dropdown-portal')).toHaveCount(0)
  await expect(page.locator('#harness-select')).toBeFocused()
})

test('Escape closes without changing the value and returns focus to the trigger', async ({ page }) => {
  await openSelect(page)

  await page.keyboard.press('ArrowDown')
  await page.keyboard.press('Escape')

  await expect(page.locator('#selected-value')).toHaveText('null')
  await expect(page.locator('.select-dropdown-portal')).toHaveCount(0)
  await expect(page.locator('#harness-select')).toBeFocused()
})

test('Tab closes the dropdown', async ({ page }) => {
  await openSelect(page)

  await page.keyboard.press('Tab')

  await expect(page.locator('.select-dropdown-portal')).toHaveCount(0)
})
