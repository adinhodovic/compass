import { defineConfig, devices } from '@playwright/test';

export default defineConfig({
  testDir: './tests/e2e',
  timeout: 30_000,
  expect: { timeout: 5_000 },
  use: {
    baseURL: 'http://127.0.0.1:18080',
    trace: 'on-first-retry',
  },
  projects: [
    {
      name: 'chromium',
      use: { ...devices['Desktop Chrome'] },
    },
  ],
  webServer: {
    command: 'go run ./cmd/compass --config tests/e2e/compass.yaml --listen-address 127.0.0.1:18080',
    url: 'http://127.0.0.1:18080',
    reuseExistingServer: !process.env.CI,
    timeout: 20_000,
  },
});
