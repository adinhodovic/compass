// Serves tests/e2e/status.json so the e2e config's `type: json` status
// provider has a real endpoint to poll.
import { createServer } from 'node:http';
import { readFileSync } from 'node:fs';

const body = readFileSync(new URL('./status.json', import.meta.url));
createServer((_, res) => {
  res.setHeader('Content-Type', 'application/json');
  res.end(body);
}).listen(18081, '127.0.0.1');
