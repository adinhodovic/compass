// compassCommand backs the cmd-K palette. compassFuse is shared with
// home.js so changing the threshold updates every fuzzy search at once.

import { safeJSONParse } from './storage.js';

const COMPASS_FUSE_THRESHOLD = 0.35;

export function compassFuse(items, keys) {
  if (!window.Fuse) return null;
  return new window.Fuse(items, { keys, threshold: COMPASS_FUSE_THRESHOLD });
}

export function compassCommand() {
  return {
    open: false,
    query: '',
    items: [],
    activeIndex: 0,
    fuse: null,
    resultCache: { query: null, items: [] },
    init() {
      const data = document.getElementById('command-data');
      this.items = data ? safeJSONParse(data.textContent, []) : [];
    },
    groupedResults() {
      const buckets = {};
      for (const item of this.results()) {
        const key = item.type === 'page' ? 'Pages' : 'Services';
        (buckets[key] = buckets[key] || []).push(item);
      }
      return Object.entries(buckets).map(([title, items]) => ({ title, items }));
    },
    openCommand() {
      this.open = true;
      this.focusInput();
    },
    focusInput() {
      this.query = '';
      this.activeIndex = 0;
      this.$nextTick(() => { this.$refs.commandInput && this.$refs.commandInput.focus(); });
    },
    results() {
      if (this.resultCache.query === this.query) return this.resultCache.items;
      let items;
      if (!this.query) items = this.items.slice(0, 50);
      if (!this.fuse) {
        this.fuse = compassFuse(this.items, ['label', 'section', 'keywords', 'type']);
      }
      if (!items) {
        if (!this.fuse) items = this.items.slice(0, 50);
        else items = this.fuse.search(this.query).map((result) => result.item).slice(0, 50);
      }
      this.resultCache = { query: this.query, items };
      return items;
    },
    move(delta) {
      const total = this.results().length;
      if (!total) return;
      this.activeIndex = (this.activeIndex + delta + total) % total;
    },
    select(item) {
      if (!item) return;
      window.location.href = item.value;
    },
    commit() {
      this.select(this.results()[this.activeIndex]);
    }
  };
}
