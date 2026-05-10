// compassHome backs the services dashboard at "/" — search, tag/source
// filters, favorites/recents, group-by, and pagination. It is the largest
// Alpine factory by far; keeping it here isolates that complexity from
// the smaller per-page factories in factories.js.

import { storageGet, storageSet, safeJSONParse } from './storage.js';
import { compassFuse } from './command.js';

const COMPASS_SORT_MODES = ['name', 'favorites'];
const COMPASS_TAG_PICKER_LIMIT_DESKTOP = 30;
const COMPASS_TAG_PICKER_LIMIT_MOBILE = 10;
const COMPASS_TAG_SEARCH_RESULTS_LIMIT = 15;

export function compassHome() {
  return {
    q: '',
    tags: [],
    sources: [],
    tagQuery: '',
    tagPickerOpen: false,
    sourcePickerOpen: false,
    sortPickerOpen: false,
    sortMode: 'name',
    pageSize: 9,
    groupShown: {},
    collapsedGroups: {},
    groupsPage: 1,
    groupsPerPage: 5,
    services: [],
    tagOptions: [],
    tagCounts: {},
    sourceOptions: [],
    favorites: [],
    recents: [],
    recentsLimit: 5,
    serviceByID: {},
    serviceNameCounts: {},
    fuse: null,
    queryCache: { q: '', ids: null },
    groupCache: { key: '', values: new Map() },
    init() {
      const data = document.getElementById('services-data');
      this.services = data ? safeJSONParse(data.textContent, []) : [];
      this.serviceByID = Object.fromEntries(this.services.map((service) => [service.id, service]));
      this.serviceNameCounts = this.services.reduce((counts, service) => {
        const key = this.serviceNameKey(service.name);
        counts[key] = (counts[key] || 0) + 1;
        return counts;
      }, {});
      this.tagCounts = this.services.reduce((counts, service) => {
        (service.tags || []).forEach((tag) => { counts[tag] = (counts[tag] || 0) + 1; });
        return counts;
      }, {});
      this.tagOptions = Object.keys(this.tagCounts).sort((a, b) => this.tagSort(a, b));
      const sourceMap = new Map();
      this.services.forEach((service) => sourceMap.set(service.sourceID, service.sourceLabel));
      this.sourceOptions = [...sourceMap.entries()]
        .map(([value, label]) => ({ value, label }))
        .sort((a, b) => a.label.localeCompare(b.label));
      const userKey = (document.body.dataset.user || 'anon').toLowerCase();
      this.storage = {
        favorites: 'compass-favorites:' + userKey,
        recents: 'compass-recents:' + userKey,
        filters: 'compass-filters:' + userKey,
        collapsed: 'compass-collapsed:' + userKey,
        sort: 'compass-sort:' + userKey,
      };
      this.favorites = safeJSONParse(storageGet(this.storage.favorites), []);
      this.recents = safeJSONParse(storageGet(this.storage.recents), []);
      const url = new URL(window.location.href);
      const stored = safeJSONParse(storageGet(this.storage.filters), {});
      this.q = url.searchParams.get('q') || stored.q || '';
      const tagsParam = url.searchParams.get('tags');
      this.tags = tagsParam ? tagsParam.split(',').filter(Boolean) : (stored.tags || []);
      const sourceParam = url.searchParams.get('source');
      this.sources = sourceParam
        ? sourceParam.split(',').filter(Boolean)
        : (stored.sources || (stored.source ? [stored.source] : []));
      this.collapsedGroups = safeJSONParse(storageGet(this.storage.collapsed), {});
      const storedSort = storageGet(this.storage.sort);
      this.sortMode = COMPASS_SORT_MODES.includes(storedSort) ? storedSort : 'name';
      this.$watch('sortMode', (v) => storageSet(this.storage.sort, v));
      this.$watch('q', () => { this.resetGroupShown(); this.groupsPage = 1; this.persistFilters(); this.syncURL(); });
      this.$watch('tags', () => { this.resetGroupShown(); this.groupsPage = 1; this.persistFilters(); this.syncURL(); });
      this.$watch('sources', () => { this.resetGroupShown(); this.groupsPage = 1; this.persistFilters(); this.syncURL(); });
      this.$watch('favorites', (v) => storageSet(this.storage.favorites, JSON.stringify(v)));
      this.$watch('recents', (v) => storageSet(this.storage.recents, JSON.stringify(v)));
      this.$watch('collapsedGroups', (v) => storageSet(this.storage.collapsed, JSON.stringify(v)));
    },
    persistFilters() {
      storageSet(this.storage.filters, JSON.stringify({
        q: this.q, tags: this.tags, sources: this.sources,
      }));
    },
    syncURL() {
      const url = new URL(window.location.href);
      const set = (key, value) => value
        ? url.searchParams.set(key, value)
        : url.searchParams.delete(key);
      set('q', this.q);
      set('tags', this.tags.join(','));
      set('source', this.sources.join(','));
      history.replaceState(null, '', url);
    },
    setGroupBy(value) {
      const url = new URL(window.location.href);
      url.searchParams.set('group', value);
      if (window.htmx) {
        // outerHTML so the response's <main> replaces the existing
        // one. With innerHTML + select=main, htmx nests the selected
        // <main> inside the target, leaving two of them in the DOM.
        window.htmx.ajax('GET', url.toString(), {
          target: 'main',
          select: 'main',
          swap: 'outerHTML',
          pushUrl: url.toString(),
        });
        return;
      }
      window.location.href = url.toString();
    },
    toggleGroup(name) {
      this.collapsedGroups = { ...this.collapsedGroups, [name]: !this.collapsedGroups[name] };
    },
    groupCollapsed(name) {
      return !!this.collapsedGroups[name];
    },
    groupOnPage(index) {
      if (this.hasFilters()) return true;
      const start = (this.groupsPage - 1) * this.groupsPerPage;
      return index >= start && index < start + this.groupsPerPage;
    },
    hasFilters() {
      return !!(this.q || this.tags.length || this.sources.length);
    },
    groupsTotalPages(totalGroups) {
      return Math.max(1, Math.ceil(totalGroups / this.groupsPerPage));
    },
    groupsPageLabel(totalGroups) {
      return this.groupsPage + ' / ' + this.groupsTotalPages(totalGroups);
    },
    nextGroupsPage(totalGroups) {
      this.groupsPage = Math.min(this.groupsPage + 1, this.groupsTotalPages(totalGroups));
      window.scrollTo({ top: 0, behavior: 'smooth' });
    },
    prevGroupsPage() {
      this.groupsPage = Math.max(this.groupsPage - 1, 1);
      window.scrollTo({ top: 0, behavior: 'smooth' });
    },
    toggleFavorite(id) {
      const index = this.favorites.indexOf(id);
      if (index >= 0) this.favorites.splice(index, 1);
      else this.favorites = [...this.favorites, id];
    },
    isFavorite(id) {
      return this.favorites.includes(id);
    },
    trackRecent(id) {
      this.recents = [id, ...this.recents.filter((x) => x !== id)].slice(0, this.recentsLimit);
    },
    clearRecents() {
      this.recents = [];
    },
    favoriteServices() {
      return this.favorites
        .map((id) => this.serviceByID[id])
        .filter(Boolean);
    },
    recentServices() {
      return this.recents
        .map((id) => this.serviceByID[id])
        .filter(Boolean);
    },
    serviceNameKey(name) {
      return (name || '').trim().toLocaleLowerCase();
    },
    duplicateServiceName(service) {
      return (this.serviceNameCounts[this.serviceNameKey(service.name)] || 0) > 1;
    },
    availableTags() {
      return this.tagOptions;
    },
    tagDisplayLimit() {
      return window.matchMedia && window.matchMedia('(max-width: 639px)').matches
        ? COMPASS_TAG_PICKER_LIMIT_MOBILE
        : COMPASS_TAG_PICKER_LIMIT_DESKTOP;
    },
    usesTagSearchPicker() {
      return this.availableTags().length > this.tagDisplayLimit();
    },
    toggleTagPicker() {
      this.tagPickerOpen = !this.tagPickerOpen;
      if (this.tagPickerOpen) {
        this.$nextTick(() => this.$refs.tagSearch?.focus());
      }
    },
    tagPickerLabel() {
      if (!this.tags.length) return 'Select tags';
      const shown = this.tags.slice(0, 5).join(', ');
      const extra = this.tags.length - 5;
      return extra > 0 ? shown + ' +' + extra : shown;
    },
    tagPickerResultLabel() {
      const total = this.filteredTagOptions().length;
      if (this.tagQuery.trim()) return total + ' matching tag' + (total === 1 ? '' : 's');
      return this.availableTags().length + ' available tag' + (this.availableTags().length === 1 ? '' : 's');
    },
    filteredTagOptions() {
      const query = this.tagQuery.trim().toLowerCase();
      const options = query
        ? this.tagOptions.filter((tag) => tag.toLowerCase().includes(query))
        : this.tagOptions;
      return [...options].sort((a, b) => {
        const selected = Number(this.hasTag(b)) - Number(this.hasTag(a));
        return selected || this.tagSort(a, b);
      });
    },
    visibleTagOptions() {
      return this.filteredTagOptions().slice(0, COMPASS_TAG_SEARCH_RESULTS_LIMIT);
    },
    tagCount(tag) {
      return this.tagCounts[tag] || 0;
    },
    tagSort(a, b) {
      return (this.tagCount(b) - this.tagCount(a)) || a.localeCompare(b);
    },
    availableSources() {
      return this.sourceOptions;
    },
    sourceLabel(value) {
      const match = this.availableSources().find((item) => item.value === value);
      return match ? match.label : value;
    },
    sourcePickerLabel() {
      if (!this.sources.length) return 'All sources';
      const shown = this.sources.slice(0, 2).map((source) => this.sourceLabel(source)).join(', ');
      const extra = this.sources.length - 2;
      return extra > 0 ? shown + ' +' + extra : shown;
    },
    toggleSource(value) {
      if (!value) return;
      this.sources = this.sources.includes(value)
        ? this.sources.filter((source) => source !== value)
        : [...this.sources, value];
    },
    removeSource(value) {
      this.sources = this.sources.filter((source) => source !== value);
      this.resetGroupShown();
    },
    hasSource(value) {
      return this.sources.includes(value);
    },
    toggleTag(value) {
      if (!value) return;
      this.tags = this.tags.includes(value)
        ? this.tags.filter((t) => t !== value)
        : [...this.tags, value];
    },
    removeTag(value) {
      this.tags = this.tags.filter((t) => t !== value);
      this.resetGroupShown();
    },
    hasTag(value) {
      return this.tags.includes(value);
    },
    visible(id) {
      const service = this.serviceByID[id];
      if (!service) return false;
      if (this.tags.length && !this.tags.some((t) => (service.tags || []).includes(t))) return false;
      if (this.sources.length && !this.sources.includes(service.sourceID)) return false;
      if (!this.q) return true;
      const ids = this.searchResultIDs();
      return ids ? ids.has(id) : this.serviceMatchesText(service);
    },
    searchResultIDs() {
      if (this.queryCache.q === this.q) return this.queryCache.ids;
      if (!this.fuse) {
        this.fuse = compassFuse(this.services, ['name', 'sourceLabel', 'source', 'tags', 'description', 'url']);
      }
      const ids = this.fuse ? new Set(this.fuse.search(this.q).map((item) => item.item.id)) : null;
      this.queryCache = { q: this.q, ids };
      return ids;
    },
    serviceMatchesText(service) {
      const q = this.q.toLowerCase();
      return [service.name, service.sourceLabel, service.source, service.description, service.url, ...(service.tags || [])]
        .some((value) => (value || '').toLowerCase().includes(q));
    },
    groupVisible(ids) {
      return ids.some((id) => this.visible(id));
    },
    anyVisible() {
      return this.services.some((service) => this.visible(service.id));
    },
    visibleCount() {
      return this.services.filter((service) => this.visible(service.id)).length;
    },
    sortedIDs(ids) {
      const services = ids.map((id) => this.serviceByID[id]).filter(Boolean);
      if (this.sortMode === 'name') {
        services.sort((a, b) => a.name.localeCompare(b.name));
      } else if (this.sortMode === 'favorites') {
        services.sort((a, b) => {
          const af = this.isFavorite(a.id), bf = this.isFavorite(b.id);
          if (af !== bf) return af ? -1 : 1;
          return a.name.localeCompare(b.name);
        });
      }
      return services.map((s) => s.id);
    },
    filteredGroupIDs(ids) {
      const key = JSON.stringify({ q: this.q, tags: this.tags, sources: this.sources, sort: this.sortMode, favorites: this.favorites });
      const groupKey = ids.join(' ');
      if (this.groupCache.key !== key) {
        this.groupCache = { key, values: new Map() };
      }
      if (!this.groupCache.values.has(groupKey)) {
        this.groupCache.values.set(groupKey, this.sortedIDs(ids).filter((id) => this.visible(id)));
      }
      return this.groupCache.values.get(groupKey);
    },
    groupShownCount(group) {
      return this.groupShown[group] || this.pageSize;
    },
    groupHasMore(ids, group) {
      return this.filteredGroupIDs(ids).length > this.groupShownCount(group);
    },
    groupHiddenCount(ids, group) {
      return Math.max(0, this.filteredGroupIDs(ids).length - this.groupShownCount(group));
    },
    visibleInGroup(id, ids, group) {
      const filtered = this.filteredGroupIDs(ids);
      return filtered.slice(0, this.groupShownCount(group)).includes(id);
    },
    showMore(group, ids) {
      const total = this.filteredGroupIDs(ids).length;
      this.groupShown = {
        ...this.groupShown,
        [group]: Math.min(this.groupShownCount(group) + this.pageSize, total),
      };
    },
    resetGroupShown() {
      this.groupShown = {};
    },
    resetFilters() {
      this.q = '';
      this.tags = [];
      this.sources = [];
      this.resetGroupShown();
    },
    focusSearch(event) {
      if (event.target.closest && event.target.closest('input, textarea, select, [contenteditable="true"]')) return;
      event.preventDefault();
      this.$refs.serviceSearch && this.$refs.serviceSearch.focus();
    },
    selectTag(value) {
      if (value && !this.tags.includes(value)) {
        this.tags = [...this.tags, value];
      }
      this.resetGroupShown();
    },
    selectSource(value) {
      if (value && !this.sources.includes(value)) {
        this.sources = [...this.sources, value];
      }
      this.resetGroupShown();
    }
  };
}
