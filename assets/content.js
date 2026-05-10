// Per-content init: code-copy buttons, mermaid diagrams, Grafana iframe
// theme sync. Runs on initial DOM ready and on every htmx swap so swapped
// fragments pick up the same enhancements as server-rendered content.

// Copy button on every <pre> inside .prose. Markdown code blocks render
// as <pre><code>...</code></pre>; we wrap with a relative positioning
// container and inject a daisy-styled button at the top-right.
function attachCodeCopyButtons(root = document) {
  const blocks = root.querySelectorAll('.prose pre');
  blocks.forEach((pre) => {
    if (pre.dataset.copyAttached === '1') return;
    pre.dataset.copyAttached = '1';
    pre.style.position = 'relative';
    const button = document.createElement('button');
    button.type = 'button';
    button.className = 'btn btn-xs btn-neutral absolute top-2 right-2 opacity-0 transition-opacity';
    button.textContent = 'Copy';
    button.setAttribute('aria-label', 'Copy code to clipboard');
    pre.addEventListener('mouseenter', () => { button.classList.remove('opacity-0'); });
    pre.addEventListener('mouseleave', () => { button.classList.add('opacity-0'); });
    button.addEventListener('click', () => {
      const code = pre.querySelector('code');
      const text = (code ? code.innerText : pre.innerText).replace(/\n$/, '');
      if (!navigator.clipboard) {
        button.textContent = 'Unavailable';
        return;
      }
      navigator.clipboard.writeText(text)
        .then(() => { button.textContent = 'Copied'; setTimeout(() => { button.textContent = 'Copy'; }, 1200); })
        .catch(() => { button.textContent = 'Failed'; setTimeout(() => { button.textContent = 'Copy'; }, 1200); });
    });
    pre.appendChild(button);
  });
}

// Mermaid: render code blocks marked `language-mermaid` as SVG diagrams.
// Lazy-loaded via the bundled UMD build (single file, no dynamic chunks)
// so pages without diagrams don't pay the ~3MB JS cost.
function loadScriptOnce(src) {
  return new Promise((resolve, reject) => {
    let s = document.querySelector(`script[data-src="${src}"]`);
    if (s) {
      if (s.dataset.loaded === '1') return resolve();
      s.addEventListener('load', () => resolve(), { once: true });
      s.addEventListener('error', reject, { once: true });
      return;
    }
    s = document.createElement('script');
    s.src = src;
    s.dataset.src = src;
    s.addEventListener('load', () => { s.dataset.loaded = '1'; resolve(); }, { once: true });
    s.addEventListener('error', reject, { once: true });
    document.head.appendChild(s);
  });
}

async function maybeRenderMermaid(root = document) {
  const blocks = root.querySelectorAll('pre code.language-mermaid');
  if (!blocks.length) return;
  try {
    await loadScriptOnce('/static/mermaid.min.js');
  } catch (err) {
    console.error('mermaid: failed to load', err);
    return;
  }
  const mermaid = window.mermaid;
  if (!mermaid) {
    console.error('mermaid: global not present after load');
    return;
  }
  const theme = document.documentElement.getAttribute('data-theme') === 'dark' ? 'dark' : 'default';
  mermaid.initialize({ startOnLoad: false, theme });
  for (const code of blocks) {
    const id = 'mermaid-' + Math.random().toString(36).slice(2);
    try {
      const { svg } = await mermaid.render(id, code.innerText);
      const wrapper = document.createElement('div');
      wrapper.className = 'mermaid not-prose my-4 flex justify-center';
      wrapper.innerHTML = svg;
      code.parentElement.replaceWith(wrapper);
    } catch (err) {
      console.error('mermaid: render failed', err);
    }
  }
}

// Sync embedded Grafana panel iframes to the current Compass theme.
// Each iframe carries its original URL on `data-base-src` (cached on
// first run) so we can rewrite a fresh `theme=` query param without
// accumulating duplicates across toggles.
export function syncGrafanaTheme(root = document) {
  const theme = document.documentElement.getAttribute('data-theme') === 'dark' ? 'dark' : 'light';
  root.querySelectorAll('iframe.grafana-panel').forEach((iframe) => {
    let base = iframe.dataset.baseSrc;
    if (!base) {
      base = (iframe.getAttribute('src') || '')
        .replace(/([?&])theme=[^&]*&?/, (_, sep) => sep)
        .replace(/[?&]$/, '');
      iframe.dataset.baseSrc = base;
    }
    if (!base) return;
    const sep = base.includes('?') ? '&' : '?';
    iframe.src = base + sep + 'theme=' + theme;
  });
}

export function initContent(root = document) {
  attachCodeCopyButtons(root);
  maybeRenderMermaid(root);
  syncGrafanaTheme(root);
}
