// Entry point. esbuild bundles the modules below into a single IIFE.
// Alpine evaluates `x-data="compassHome()"` against the page's global
// scope, so each factory is re-exposed on `window.*`. The keyboard
// shortcuts, htmx-swap fan-out, and theme listener live here because
// they are page-wide and don't fit any one factory.

import { initContent, syncGrafanaTheme } from './content.js';
import { compassShell, debugPagination, serviceNotes } from './factories.js';
import { compassCommand } from './command.js';
import { compassHome } from './home.js';

window.compassShell = compassShell;
window.compassHome = compassHome;
window.compassCommand = compassCommand;
window.debugPagination = debugPagination;
window.serviceNotes = serviceNotes;

window.addEventListener('keydown', (event) => {
  if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'k') {
    event.preventDefault();
    window.dispatchEvent(new CustomEvent('open-command'));
  } else if ((event.metaKey || event.ctrlKey) && (event.key === '/' || event.code === 'Slash')) {
    event.preventDefault();
    window.dispatchEvent(new CustomEvent('focus-search'));
  }
}, { capture: true });

document.addEventListener('DOMContentLoaded', () => initContent());
document.addEventListener('htmx:afterSwap', (event) => initContent(event.target));
window.addEventListener('compass:theme', () => syncGrafanaTheme());
