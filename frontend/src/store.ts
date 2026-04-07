/**
 * Unified app store. Go owns all state; this is a read-through cache.
 * Event listeners are registered ONCE here -- components NEVER call EventsOn.
 * See wails.md for the architecture.
 */

import { globToRegex } from './utils/glob';

// Types
export interface Status {
  connected: boolean;
  classification: string;
  classifierTier: string;
  whisperLoad: number;  // 0-1+ ratio of processing time to audio duration
}

export interface Download {
  active: boolean;
  percent: number;
  message: string;
}

export interface Entry {
  id: number;
  type: 'speech' | 'song' | 'music_unknown';
  timestamp: Date;
  content: string;
  title: string;
  artist: string;
  fresh?: boolean;
}

// State
let _status: Status = { connected: false, classification: '--', classifierTier: '', whisperLoad: 0 };
let _entries: Entry[] = [];
let _download: Download = { active: false, percent: 0, message: '' };
let _gpuWarning = '';
let _selected: Set<number> = new Set();

// Search (local UI state, not from Go)
let _searchQuery = '';
let _searchRegex: RegExp | null = null;
let _filterActive = false;

// Subscribers
type Callback = () => void;
let _listeners: Set<Callback> = new Set();
let _initialized = false;

function notify() {
  for (const fn of _listeners) fn();
}

export function subscribe(fn: Callback): () => void {
  _listeners.add(fn);
  return () => { _listeners.delete(fn); };
}

// Getters
export function getStatus(): Status { return _status; }
export function getEntries(): Entry[] { return _entries; }
export function getDownload(): Download { return _download; }
export function getGPUWarning(): string { return _gpuWarning; }
export function getSelected(): Set<number> { return _selected; }
export function getSelectedEntries(): Entry[] { return _entries.filter(e => _selected.has(e.id)); }
export function isSelected(id: number): boolean { return _selected.has(id); }
export function getSearchQuery(): string { return _searchQuery; }
export function getSearchRegex(): RegExp | null { return _searchRegex; }
export function getFilterActive(): boolean { return _filterActive; }

// Search mutations (local UI, no Go involvement)
export function setSearchQuery(q: string) {
  _searchQuery = q;
  _searchRegex = globToRegex(q);
  notify();
}
export function toggleFilter() { _filterActive = !_filterActive; notify(); }
export function clearSearch() { _searchQuery = ''; _searchRegex = null; _filterActive = false; notify(); }
export function matchesEntry(entry: Entry): boolean {
  if (!_searchRegex) return true;
  const searchable = entry.type === 'song' || entry.type === 'music_unknown'
    ? `${entry.title} ${entry.artist} ${entry.content}`
    : entry.content;
  return _searchRegex.test(searchable);
}

// Selection mutations
export function toggleSelect(id: number, shiftKey: boolean) {
  if (shiftKey && _selected.size > 0) {
    const lastId = Array.from(_selected).pop()!;
    const lastIdx = _entries.findIndex(e => e.id === lastId);
    const thisIdx = _entries.findIndex(e => e.id === id);
    if (lastIdx !== -1 && thisIdx !== -1) {
      const [start, end] = lastIdx < thisIdx ? [lastIdx, thisIdx] : [thisIdx, lastIdx];
      for (let i = start; i <= end; i++) _selected.add(_entries[i].id);
    }
  } else {
    if (_selected.has(id)) _selected.delete(id); else _selected.add(id);
  }
  notify();
}
export function clearSelection() { _selected = new Set(); notify(); }

// Entry mutations (for optimistic UI updates from user actions)
export function updateEntry(id: number, updates: Partial<Entry>) {
  _entries = _entries.map(e => e.id === id ? { ...e, ...updates } : e);
  notify();
}
export function removeEntry(id: number) {
  _entries = _entries.filter(e => e.id !== id);
  _selected.delete(id);
  notify();
}
export function removeEntries(ids: number[]) {
  const s = new Set(ids);
  _entries = _entries.filter(e => !s.has(e.id));
  for (const id of ids) _selected.delete(id);
  notify();
}
export function insertAfter(afterId: number, entry: Entry) {
  const idx = _entries.findIndex(e => e.id === afterId);
  if (idx === -1) { _entries = [..._entries, entry]; }
  else { const n = [..._entries]; n.splice(idx + 1, 0, entry); _entries = n; }
  notify();
}
export function appendEntry(entry: Entry) {
  _entries = [..._entries, entry];
  if (_entries.length > 200) _entries = _entries.slice(-200);
  notify();
}

// Streaming state (derived from status but can be set optimistically)
let _streaming = false;
let _listenEnabled = false;

export function getStreaming(): boolean { return _streaming; }
export function getListenEnabled(): boolean { return _listenEnabled; }
export function setStreaming(s: boolean) { _streaming = s; notify(); }
export function setListenEnabled(enabled: boolean) { _listenEnabled = enabled; notify(); }

// Convert Go entry (from initial state or events) to frontend Entry
function toEntry(le: any): Entry {
  return {
    id: le.id ?? le.ID ?? 0,
    type: (le.type ?? le.Type ?? le.EntryType ?? 'speech') as Entry['type'],
    timestamp: new Date(le.timestamp ?? le.Timestamp),
    content: le.content ?? le.Content ?? '',
    title: le.title ?? le.Title ?? '',
    artist: le.artist ?? le.Artist ?? '',
    fresh: le.fresh ?? le.Fresh ?? false,
  };
}

/**
 * Initialize the store. Called ONCE from App.svelte onMount.
 * 1. Fetches initial state from Go (catches up on missed events)
 * 2. Registers Wails event listeners (globally, forever)
 * 3. Notifies all subscribers
 */
export async function init() {
  if (_initialized) return;
  _initialized = true;

  // 1. Catch up: fetch initial state from Go
  try {
    const { GetInitialState } = await import('../wailsjs/go/main/App');
    const state = await GetInitialState();
    if (state) {
      _status = {
        connected: state.Connected || false,
        classification: state.Classification || '--',
        classifierTier: state.ClassifierTier || '',
        whisperLoad: state.WhisperLoad || 0,
      };
      _streaming = state.Connected || false;
      _entries = (state.Entries || []).map(toEntry);
      _download = {
        active: state.Downloading || false,
        percent: state.DownloadPercent || 0,
        message: state.DownloadMessage || '',
      };
      _gpuWarning = state.GPUWarning || '';
    }
  } catch (e) {
    console.error('Failed to fetch initial state:', e);
    // Fallback: load entries from DB directly
    try {
      const { GetRecentEntries } = await import('../wailsjs/go/main/App');
      const recent = await GetRecentEntries(200);
      if (recent) {
        _entries = recent.reverse().map(toEntry);
      }
    } catch { /* ignore */ }
  }

  // 2. Register Wails event listeners ONCE
  try {
    const { EventsOn } = await import('../wailsjs/runtime/runtime');

    EventsOn('status:update', (data: any) => {
      _status = {
        connected: data.connected,
        classification: data.classification || '--',
        classifierTier: data.classifierTier || _status.classifierTier,
        whisperLoad: data.whisperLoad ?? _status.whisperLoad,
      };
      _streaming = data.connected;
      notify();
    });

    EventsOn('entry:new', (data: any) => {
      const entry = toEntry(data);
      _entries = [..._entries, entry];
      if (_entries.length > 200) _entries = _entries.slice(-200);
      notify();
    });

    EventsOn('entry:update', (data: any) => {
      const id = data.id;
      const updates = data.updates || {};
      _entries = _entries.map(e => e.id === id ? { ...e, ...updates } : e);
      notify();
    });

    EventsOn('entry:remove', (data: any) => {
      _entries = _entries.filter(e => e.id !== data.id);
      _selected.delete(data.id);
      notify();
    });

    EventsOn('download:progress', (data: any) => {
      _download = { active: true, percent: data.percent || 0, message: data.message || '' };
      notify();
    });

    EventsOn('download:complete', () => {
      _download = { active: false, percent: 100, message: '' };
      notify();
    });

    EventsOn('app:gpuWarning', (msg: string) => {
      _gpuWarning = msg;
      notify();
    });

    console.log('Store initialized, events registered');
  } catch (e) {
    console.error('Failed to register events:', e);
  }

  // 3. Notify with initial data
  notify();
}
