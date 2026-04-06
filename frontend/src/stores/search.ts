/**
 * Search state store.
 */
import { globToRegex } from '../utils/glob';
import type { Entry } from './entries';

let _query = '';
let _filterActive = false;
let _regex: RegExp | null = null;
let _listeners: Array<() => void> = [];

function notify() {
  for (const fn of _listeners) fn();
}

export function getQuery(): string {
  return _query;
}

export function getFilterActive(): boolean {
  return _filterActive;
}

export function getRegex(): RegExp | null {
  return _regex;
}

export function subscribe(fn: () => void): () => void {
  _listeners.push(fn);
  return () => {
    _listeners = _listeners.filter(l => l !== fn);
  };
}

export function setQuery(q: string) {
  _query = q;
  _regex = globToRegex(q);
  notify();
}

export function toggleFilter() {
  _filterActive = !_filterActive;
  notify();
}

export function clearSearch() {
  _query = '';
  _filterActive = false;
  _regex = null;
  notify();
}

/**
 * Check if an entry matches the current search.
 * Returns true if no search is active.
 */
export function matchesEntry(entry: Entry): boolean {
  if (!_regex) return true;
  const searchable = entryText(entry);
  return _regex.test(searchable);
}

function entryText(entry: Entry): string {
  if (entry.type === 'song' || entry.type === 'music_unknown') {
    return `${entry.title} ${entry.artist} ${entry.content}`;
  }
  return entry.content;
}
