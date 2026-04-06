/**
 * Reactive store for transcript entries.
 * Uses Svelte 5 module-level $state runes.
 */

export interface Entry {
  id: number;
  type: 'speech' | 'song' | 'music_unknown';
  timestamp: Date;
  content: string;
  title: string;
  artist: string;
}

// WHY: Module-level state with getter/setter functions rather than a class.
// Svelte 5 runes ($state) work at the module level in .svelte.ts files,
// but in plain .ts files we need to export mutable references that components
// can import. We use a simple array and mutation functions.
let _entries: Entry[] = [];
let _listeners: Array<() => void> = [];

function notify() {
  for (const fn of _listeners) fn();
}

export function getEntries(): Entry[] {
  return _entries;
}

export function subscribe(fn: () => void): () => void {
  _listeners.push(fn);
  return () => {
    _listeners = _listeners.filter(l => l !== fn);
  };
}

export function setEntries(entries: Entry[]) {
  _entries = entries;
  notify();
}

export function appendEntry(entry: Entry) {
  _entries = [..._entries, entry];
  notify();
}

export function updateEntry(id: number, updates: Partial<Entry>) {
  _entries = _entries.map(e =>
    e.id === id ? { ...e, ...updates } : e
  );
  notify();
}

export function insertAfter(afterId: number, entry: Entry) {
  const idx = _entries.findIndex(e => e.id === afterId);
  if (idx === -1) {
    // Append if not found
    _entries = [..._entries, entry];
  } else {
    const next = [..._entries];
    next.splice(idx + 1, 0, entry);
    _entries = next;
  }
  notify();
}

/**
 * Convert a Go LogEntry to our frontend Entry type.
 */
export function fromLogEntry(le: any): Entry {
  return {
    id: le.ID,
    type: le.EntryType === 'song' ? 'song'
      : le.EntryType === 'music_unknown' ? 'music_unknown'
      : 'speech',
    timestamp: new Date(le.Timestamp),
    content: le.Content || '',
    title: le.Title || '',
    artist: le.Artist || '',
  };
}
