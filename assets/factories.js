// Small Alpine x-data factories that don't warrant their own file:
// - compassShell: top-level theme/menu/toast state for the layout.
// - debugPagination: per-source pagination on the /debug page.
// - serviceNotes: per-service per-user notes on /services/:id.

import { storageGet, storageSet, storageRemove, safeJSONParse } from './storage.js';

export function compassShell() {
  const stored = storageGet('compass-theme');
  const initialTheme = (stored === 'light' || stored === 'dark')
    ? stored
    : (window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light');
  return {
    theme: initialTheme,
    mobileMenuOpen: false,
    toast: { show: false, message: '' },
    toastTimer: null,
    setTheme(value) {
      this.theme = value === 'dark' ? 'dark' : 'light';
      storageSet('compass-theme', this.theme);
      document.documentElement.setAttribute('data-theme', this.theme);
      window.dispatchEvent(new CustomEvent('compass:theme', { detail: { theme: this.theme } }));
    },
    toggleTheme() {
      this.setTheme(this.theme === 'dark' ? 'light' : 'dark');
    },
    toggleMobileMenu() {
      this.mobileMenuOpen = !this.mobileMenuOpen;
    },
    closeMobileMenu() {
      this.mobileMenuOpen = false;
    },
    notify(message) {
      this.toast.message = message;
      this.toast.show = true;
      if (this.toastTimer) clearTimeout(this.toastTimer);
      this.toastTimer = setTimeout(() => { this.toast.show = false; }, 1400);
    },
    copy(value) {
      if (!navigator.clipboard) {
        this.notify('Copy unavailable');
        return;
      }
      navigator.clipboard.writeText(value)
        .then(() => this.notify('Copied'))
        .catch(() => this.notify('Copy failed'));
    }
  };
}

export function debugPagination() {
  const pageSize = 10;
  const sourcesPageSize = 5;
  return {
    shown: {},
    sourcePage: 1,
    sources: [],          // multi-select source filter (array of source IDs <type>/<name>)
    sourceOptions: [],    // [{value, label}] from server-rendered JSON
    sourcePickerOpen: false,
    statusFilter: '',
    init() {
      const data = document.getElementById('debug-sources');
      this.sourceOptions = data ? safeJSONParse(data.textContent, []) : [];
    },
    shownCount(name) {
      return this.shown[name] || pageSize;
    },
    visible(name, index) {
      return index < this.shownCount(name);
    },
    hasMore(name, count) {
      return count > this.shownCount(name);
    },
    hiddenCount(name, count) {
      return Math.max(0, count - this.shownCount(name));
    },
    showMore(name, count) {
      this.shown = { ...this.shown, [name]: Math.min(this.shownCount(name) + pageSize, count) };
    },
    sourcesTotalPages(count) {
      return Math.max(1, Math.ceil(count / sourcesPageSize));
    },
    sourcesPageLabel(count) {
      return this.sourcePage + ' / ' + this.sourcesTotalPages(count);
    },
    nextSourcesPage(count) {
      this.sourcePage = Math.min(this.sourcePage + 1, this.sourcesTotalPages(count));
      window.scrollTo({ top: 0, behavior: 'smooth' });
    },
    prevSourcesPage() {
      this.sourcePage = Math.max(this.sourcePage - 1, 1);
      window.scrollTo({ top: 0, behavior: 'smooth' });
    },
    sourceOnPage(index) {
      const start = (this.sourcePage - 1) * sourcesPageSize;
      return index >= start && index < start + sourcesPageSize;
    },
    // Filter helpers. When any filter is set, pagination is bypassed
    // and every matching section is shown — at homelab scale the
    // filtered set is small enough that paging through it adds friction
    // rather than removing it.
    hasFilters() {
      return this.sources.length > 0 || this.statusFilter !== '';
    },
    sectionVisible(name, errored) {
      if (this.sources.length && !this.sources.includes(name)) return false;
      if (this.statusFilter === 'ok' && errored) return false;
      if (this.statusFilter === 'error' && !errored) return false;
      return true;
    },
    resetFilters() {
      this.sources = [];
      this.statusFilter = '';
    },
    // Source picker — mirrors compassHome's source filter shape so users
    // get the same multi-select dropdown on the debug page.
    availableSources() {
      return this.sourceOptions;
    },
    sourceLabel(value) {
      const match = this.sourceOptions.find((item) => item.value === value);
      return match ? match.label : value;
    },
    sourcePickerLabel() {
      if (!this.sources.length) return 'All sources';
      const shown = this.sources.slice(0, 2).map((v) => this.sourceLabel(v)).join(', ');
      const extra = this.sources.length - 2;
      return extra > 0 ? shown + ' +' + extra : shown;
    },
    toggleSource(value) {
      if (!value) return;
      this.sources = this.sources.includes(value)
        ? this.sources.filter((s) => s !== value)
        : [...this.sources, value];
    },
    removeSource(value) {
      this.sources = this.sources.filter((s) => s !== value);
    },
    hasSource(value) {
      return this.sources.includes(value);
    },
  };
}

// serviceNotes is per-service per-user free-form text, persisted to
// localStorage only when the server provides a storage scope. Fully open
// Compass uses an "open" scope; optional-auth anonymous users get no scope.
export function serviceNotes(serviceID = '') {
  return {
    editing: false,
    note: '',
    draft: '',
    storageKey: '',
    persist: false,
    init(id = serviceID) {
      const user = (document.body.dataset.user || '').trim().toLowerCase();
      this.persist = user !== '';
      if (this.persist) {
        this.storageKey = 'compass-note:' + user + ':' + id;
        this.note = storageGet(this.storageKey, '') || '';
        this.draft = this.note;
      }
    },
    save() {
      this.note = this.draft;
      if (this.persist) {
        if (this.note) storageSet(this.storageKey, this.note);
        else storageRemove(this.storageKey);
      }
      this.editing = false;
    },
  };
}
