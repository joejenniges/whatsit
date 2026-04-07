/**
 * Reactive store for transcript entries.
 */

export interface Entry {
  id: number;
  type: 'speech' | 'song' | 'music_unknown';
  timestamp: Date;
  content: string;
  title: string;
  artist: string;
  fresh?: boolean;
}

let _entries: Entry[] = [];
let _selected: Set<number> = new Set();
let _listeners: Array<() => void> = [];
let _eventsSetup = false;

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

export function removeEntry(id: number) {
  _entries = _entries.filter(e => e.id !== id);
  _selected.delete(id);
  notify();
}

export function removeEntries(ids: number[]) {
  const idSet = new Set(ids);
  _entries = _entries.filter(e => !idSet.has(e.id));
  for (const id of ids) _selected.delete(id);
  notify();
}

export function isSelected(id: number): boolean {
  return _selected.has(id);
}

export function getSelected(): Set<number> {
  return _selected;
}

export function getSelectedEntries(): Entry[] {
  return _entries.filter(e => _selected.has(e.id));
}

export function toggleSelect(id: number, shiftKey: boolean) {
  if (shiftKey && _selected.size > 0) {
    // Shift+click: select range from last selected to this one
    const lastId = Array.from(_selected).pop()!;
    const lastIdx = _entries.findIndex(e => e.id === lastId);
    const thisIdx = _entries.findIndex(e => e.id === id);
    if (lastIdx !== -1 && thisIdx !== -1) {
      const [start, end] = lastIdx < thisIdx ? [lastIdx, thisIdx] : [thisIdx, lastIdx];
      for (let i = start; i <= end; i++) {
        _selected.add(_entries[i].id);
      }
    }
  } else {
    if (_selected.has(id)) {
      _selected.delete(id);
    } else {
      _selected.add(id);
    }
  }
  notify();
}

export function clearSelection() {
  _selected = new Set();
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
 * Set up Wails event listeners for real-time entry updates.
 * Called once from LiveView's onMount. Idempotent -- subsequent calls are no-ops.
 * WHY here instead of in the component: event listeners are global (Wails-level).
 * If they were in the component, destroying and remounting LiveView (tab switch)
 * would register duplicate listeners or lose them entirely.
 */
export async function setupEntryEvents() {
  if (_eventsSetup) return;
  _eventsSetup = true;

  try {
    const { EventsOn } = await import('../../wailsjs/runtime/runtime');

    EventsOn('transcription', (data: any) => {
      console.log('EVENT transcription:', data);
      appendEntry({
        id: data.id,
        type: 'speech',
        timestamp: new Date(data.timestamp),
        content: data.text,
        title: '',
        artist: '',
        fresh: true,
      });
    });

    EventsOn('song-identified', (data: any) => {
      console.log('EVENT song-identified:', data);
      updateEntry(data.id, {
        type: 'song',
        title: data.title,
        artist: data.artist,
        content: `"${data.title}" - ${data.artist}`,
      });
    });

    EventsOn('music-detected', (data: any) => {
      console.log('EVENT music-detected:', data);
      appendEntry({
        id: data.id || Date.now(),
        type: 'music_unknown',
        timestamp: new Date(),
        content: 'Song played',
        title: '',
        artist: '',
        fresh: true,
      });
    });

    console.log('Entry events registered');
  } catch (e) {
    console.error('Failed to set up entry events:', e);
  }
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
