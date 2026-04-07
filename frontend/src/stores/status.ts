/**
 * Connection and streaming status store.
 */

let _connected = false;
let _classification = '--';
let _streaming = false;
let _listenEnabled = false;
let _listeners: Array<() => void> = [];
let _eventsSetup = false;

function notify() {
  for (const fn of _listeners) fn();
}

export function getConnected(): boolean { return _connected; }
export function getClassification(): string { return _classification; }
export function getStreaming(): boolean { return _streaming; }
export function getListenEnabled(): boolean { return _listenEnabled; }

export function subscribe(fn: () => void): () => void {
  _listeners.push(fn);
  return () => {
    _listeners = _listeners.filter(l => l !== fn);
  };
}

export function setStatus(connected: boolean, classification: string) {
  _connected = connected;
  _classification = classification || '--';
  // Derive streaming state from the Go backend's connected status.
  // This is the source of truth -- if the backend says connected, we're streaming.
  _streaming = connected;
  notify();
}

export function setStreaming(s: boolean) {
  _streaming = s;
  notify();
}

export function setListenEnabled(enabled: boolean) {
  _listenEnabled = enabled;
  notify();
}

/**
 * Set up Wails event listener for status updates. Idempotent.
 */
export async function setupStatusEvents() {
  if (_eventsSetup) return;
  _eventsSetup = true;

  try {
    const { EventsOn } = await import('../../wailsjs/runtime/runtime');
    EventsOn('status', (data: any) => {
      setStatus(data.connected, data.classification);
    });
    console.log('Status events registered');
  } catch (e) {
    console.error('Failed to set up status events:', e);
  }
}
