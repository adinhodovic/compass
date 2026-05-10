// localStorage wrappers swallow errors silently in normal operation
// (private-mode windows, quota exceeded, storage disabled) but warn once
// per session per failure mode so misbehaving deployments don't quietly
// lose user state.
const storageWarned = new Set();
function storageWarn(op, err) {
  const key = op + ':' + (err && err.name ? err.name : 'unknown');
  if (storageWarned.has(key)) return;
  storageWarned.add(key);
  console.warn('compass: localStorage ' + op + ' failed', err);
}

export function storageGet(key, fallback = null) {
  try {
    const value = localStorage.getItem(key);
    return value === null ? fallback : value;
  } catch (err) {
    storageWarn('get', err);
    return fallback;
  }
}

export function storageSet(key, value) {
  try { localStorage.setItem(key, value); }
  catch (err) { storageWarn('set', err); }
}

export function storageRemove(key) {
  try { localStorage.removeItem(key); }
  catch (err) { storageWarn('remove', err); }
}

export function safeJSONParse(text, fallback) {
  if (!text) return fallback;
  try { return JSON.parse(text); } catch { return fallback; }
}
