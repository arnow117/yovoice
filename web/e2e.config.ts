import type { E2EConfig } from 'e2e';
import { web } from '@e2e-dev/web';

const app = {
  url: 'http://127.0.0.1:0',
  command: {
    executable: 'node',
    args: ['./node_modules/vite/bin/vite.js', '--host', '127.0.0.1', '--port', '{port}', '--strictPort'],
    env: { E2E_TELEMETRY_DISABLED: '1' },
    log: 'e2e-results/vite.log',
  },
};

export default {
  projectId: 'yovoice-speaker-ui',
  tests: ['e2e/**/*.e2e.ts'],
  timeout: 60_000,
  actionTimeout: 15_000,
  assertionTimeout: 10_000,
  cleanupTimeout: 15_000,
  workers: 1,
  retries: 0,
  cache: 'read-only',
  output: 'e2e-results',
  reporters: ['list', 'junit'],
  targets: [
    {
      name: 'mac-zh-1280',
      engine: web({ browser: 'chromium', locale: 'zh-CN', viewport: { width: 1280, height: 840 }, initScripts: [{ path: 'e2e/synthetic-native-bridge.js' }] }),
      app,
    },
    {
      name: 'mac-en-390',
      engine: web({ browser: 'chromium', locale: 'en-US', viewport: { width: 390, height: 600 }, initScripts: [{ path: 'e2e/synthetic-native-bridge.js' }] }),
      app,
    },
  ],
} satisfies E2EConfig;
