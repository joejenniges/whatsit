<script lang="ts">
  import { onMount } from 'svelte';
  import SearchBar from './SearchBar.svelte';
  import TranscriptLine from './TranscriptLine.svelte';
  import SongLine from './SongLine.svelte';
  import InsertZone from './InsertZone.svelte';
  import ScrollToBottom from './ScrollToBottom.svelte';
  import StatusBar from './StatusBar.svelte';
  import {
    getEntries, appendEntry, updateEntry, removeEntry, removeEntries,
    insertAfter, setEntries, fromLogEntry,
    isSelected, getSelectedEntries, toggleSelect, clearSelection,
    subscribe as entriesSub,
    type Entry,
  } from '../stores/entries';
  import {
    getQuery, getFilterActive, getRegex,
    setQuery, toggleFilter, clearSearch, matchesEntry,
    subscribe as searchSub,
  } from '../stores/search';
  import {
    getConnected, getClassification, getStreaming, getListenEnabled,
    setStatus, setStreaming, setListenEnabled,
    subscribe as statusSub,
  } from '../stores/status';

  // Reactive state driven by stores.
  // WHY tick counter: Svelte 5's $state reactivity doesn't always detect
  // reassignment from manual store callbacks. The tick forces a re-render
  // by changing a value the template depends on (via $derived).
  let tick = $state(0);
  let entries: Entry[] = $state([]);
  let query = $state('');
  let filterActive = $state(false);
  let regex: RegExp | null = $state(null);
  let connected = $state(false);
  let classification = $state('--');
  let streaming = $state(false);
  let listenEnabled = $state(false);
  let selectedCount = $state(0);

  // Scroll state
  let scrollContainer: HTMLDivElement | undefined = $state(undefined);
  let autoScroll = $state(true);
  let showScrollBtn = $state(false);

  // Filtered entries
  let visibleEntries = $derived(
    filterActive && regex
      ? entries.filter(e => matchesEntry(e))
      : entries
  );

  function syncFromStores() {
    entries = [...getEntries()];  // new array reference to ensure Svelte detects the change
    query = getQuery();
    filterActive = getFilterActive();
    regex = getRegex();
    connected = getConnected();
    classification = getClassification();
    streaming = getStreaming();
    listenEnabled = getListenEnabled();
    selectedCount = getSelectedEntries().length;
    tick++;
  }

  function scrollToBottom() {
    if (scrollContainer) {
      scrollContainer.scrollTop = scrollContainer.scrollHeight;
    }
  }

  function handleScroll() {
    if (!scrollContainer) return;
    const { scrollTop, scrollHeight, clientHeight } = scrollContainer;
    const atBottom = scrollHeight - scrollTop - clientHeight < 50;
    if (atBottom) {
      autoScroll = true;
      showScrollBtn = false;
    } else {
      autoScroll = false;
      showScrollBtn = true;
    }
  }

  function handleScrollToBottom() {
    autoScroll = true;
    showScrollBtn = false;
    scrollToBottom();
  }

  // WHY: $effect for auto-scroll. When entries change and autoScroll is on,
  // we need to scroll to bottom after the DOM updates. Using tick() would be
  // ideal but requestAnimationFrame is simpler here and works reliably.
  $effect(() => {
    // Touch entries.length to subscribe to changes
    const _len = entries.length;
    if (autoScroll && _len > 0) {
      requestAnimationFrame(scrollToBottom);
    }
  });

  onMount(() => {
    syncFromStores();
    const unsubs = [
      entriesSub(syncFromStores),
      searchSub(syncFromStores),
      statusSub(syncFromStores),
    ];

    loadHistory();
    setupEvents();

    return () => {
      for (const u of unsubs) u();
    };
  });

  async function loadHistory() {
    try {
      const { GetRecentEntries } = await import('../../wailsjs/go/main/App');
      const recent = await GetRecentEntries(200);
      if (recent && recent.length > 0) {
        // Recent comes newest-first, reverse to chronological
        const mapped = recent.reverse().map(fromLogEntry);
        setEntries(mapped);
      }
    } catch {
      // Bindings not available
    }
  }

  async function setupEvents() {
    try {
      const { EventsOn } = await import('../../wailsjs/runtime/runtime');

      EventsOn('transcription', (data: any) => {
        appendEntry({
          id: data.id,
          type: 'speech',
          timestamp: new Date(data.timestamp),
          content: data.text,
          title: '',
          artist: '',
        });
      });

      EventsOn('song-identified', (data: any) => {
        updateEntry(data.id, {
          type: 'song',
          title: data.title,
          artist: data.artist,
          content: `"${data.title}" - ${data.artist}`,
        });
      });

      EventsOn('music-detected', (data: any) => {
        appendEntry({
          id: data.id,
          type: 'music_unknown',
          timestamp: new Date(),
          content: 'Song played',
          title: '',
          artist: '',
        });
      });

      EventsOn('status', (data: any) => {
        setStatus(data.connected, data.classification);
      });
    } catch {
      // Runtime not available in dev
    }
  }

  async function handleSongSave(id: number, title: string, artist: string) {
    updateEntry(id, { title, artist, type: 'song' });
    try {
      const { UpdateEntrySong } = await import('../../wailsjs/go/main/App');
      await UpdateEntrySong(id, title, artist);
    } catch {
      // Binding not available
    }
  }

  async function handleInsertSong() {
    try {
      const { InsertSongEntry } = await import('../../wailsjs/go/main/App');
      const entry = await InsertSongEntry();
      if (entry) {
        appendEntry(fromLogEntry(entry));
      }
    } catch {
      // Fallback: insert locally with a temp id
      appendEntry({
        id: Date.now(),
        type: 'music_unknown',
        timestamp: new Date(),
        content: 'Song played',
        title: '',
        artist: '',
      });
    }
  }

  async function handleInsertAfter(afterId: number) {
    try {
      const { InsertSongEntry } = await import('../../wailsjs/go/main/App');
      const entry = await InsertSongEntry();
      if (entry) {
        insertAfter(afterId, fromLogEntry(entry));
        return;
      }
    } catch {
      // Fallback
    }
    insertAfter(afterId, {
      id: Date.now(),
      type: 'music_unknown',
      timestamp: new Date(),
      content: 'Song played',
      title: '',
      artist: '',
    });
  }

  async function handleDelete(id: number) {
    removeEntry(id);
    try {
      const { DeleteEntry } = await import('../../wailsjs/go/main/App');
      await DeleteEntry(id);
    } catch {
      // Binding not available
    }
  }

  async function handleEditContent(id: number, content: string) {
    updateEntry(id, { content });
    try {
      const { UpdateEntryContent } = await import('../../wailsjs/go/main/App');
      await UpdateEntryContent(id, content);
    } catch {
      // Binding not available
    }
  }

  function handleToggleSelect(id: number, shiftKey: boolean) {
    toggleSelect(id, shiftKey);
  }

  async function handleBulkDelete() {
    const selected = getSelectedEntries();
    if (selected.length === 0) return;
    const ids = selected.map(e => e.id);
    removeEntries(ids);
    clearSelection();
    try {
      const { DeleteEntry } = await import('../../wailsjs/go/main/App');
      for (const id of ids) {
        await DeleteEntry(id);
      }
    } catch {
      // Binding not available
    }
  }

  function handleCopySelected() {
    const selected = getSelectedEntries();
    if (selected.length === 0) return;
    const text = selected.map(e => {
      const ts = e.timestamp.toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
      if (e.type === 'speech') return `[${ts}] ${e.content}`;
      if (e.title && e.artist) return `[${ts}] ${e.title} - ${e.artist}`;
      return `[${ts}] ${e.content || 'Song played'}`;
    }).join('\n');
    navigator.clipboard.writeText(text);
    clearSelection();
  }

  function handleKeydown(e: KeyboardEvent) {
    // Ctrl+A: select all visible entries
    if ((e.ctrlKey || e.metaKey) && e.key === 'a' && document.activeElement?.tagName !== 'INPUT') {
      e.preventDefault();
      for (const entry of visibleEntries) {
        toggleSelect(entry.id, false);
      }
    }
    // Delete/Backspace: delete selected
    if ((e.key === 'Delete' || e.key === 'Backspace') && getSelectedEntries().length > 0 && document.activeElement?.tagName !== 'INPUT') {
      e.preventDefault();
      handleBulkDelete();
    }
    // Ctrl+C: copy selected
    if ((e.ctrlKey || e.metaKey) && e.key === 'c' && getSelectedEntries().length > 0) {
      handleCopySelected();
    }
    // Escape: clear selection
    if (e.key === 'Escape') {
      clearSelection();
    }
  }

  async function handleStart() {
    try {
      const { StartStreaming } = await import('../../wailsjs/go/main/App');
      await StartStreaming();
      setStreaming(true);
    } catch {
      // Binding not available
    }
  }

  async function handleStop() {
    try {
      const { StopStreaming } = await import('../../wailsjs/go/main/App');
      await StopStreaming();
      setStreaming(false);
    } catch {
      // Binding not available
    }
  }

  async function handleListenToggle(enabled: boolean) {
    setListenEnabled(enabled);
    try {
      const { SetListenEnabled } = await import('../../wailsjs/go/main/App');
      await SetListenEnabled(enabled);
    } catch {
      // Binding not available
    }
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="live-view" onkeydown={handleKeydown} tabindex="-1">
  {#if selectedCount > 0}
    <div class="selection-toolbar">
      <span>{selectedCount} selected</span>
      <button onclick={handleCopySelected}>Copy</button>
      <button class="danger" onclick={handleBulkDelete}>Delete</button>
      <button onclick={() => clearSelection()}>Clear</button>
    </div>
  {/if}

  <SearchBar
    {query}
    {filterActive}
    onquery={setQuery}
    ontogglefilter={toggleFilter}
    onclear={clearSearch}
    oninsertsong={handleInsertSong}
  />

  <div class="scroll-area" bind:this={scrollContainer} onscroll={handleScroll}>
    {#each visibleEntries as entry, i (entry.id)}
      {#if i > 0}
        <InsertZone oninsert={() => handleInsertAfter(visibleEntries[i - 1].id)} />
      {/if}

      {#if entry.type === 'speech'}
        <TranscriptLine
          id={entry.id}
          timestamp={entry.timestamp}
          content={entry.content}
          {regex}
          selected={isSelected(entry.id)}
          ontoggleselect={handleToggleSelect}
          ondelete={() => handleDelete(entry.id)}
          onedit={handleEditContent}
        />
      {:else}
        <SongLine
          id={entry.id}
          title={entry.title}
          artist={entry.artist}
          content={entry.content}
          entryType={entry.type}
          timestamp={entry.timestamp}
          {regex}
          selected={isSelected(entry.id)}
          ontoggleselect={handleToggleSelect}
          onsave={handleSongSave}
          ondelete={() => handleDelete(entry.id)}
        />
      {/if}
    {/each}

    {#if entries.length === 0}
      <div class="empty-state">No entries yet. Start streaming to see transcriptions.</div>
    {/if}

    {#if filterActive && regex && visibleEntries.length === 0 && entries.length > 0}
      <div class="empty-state">No matching entries.</div>
    {/if}

  </div>

  <ScrollToBottom visible={showScrollBtn} onclick={handleScrollToBottom} />

  <StatusBar
    {connected}
    {classification}
    {streaming}
    {listenEnabled}
    onstart={handleStart}
    onstop={handleStop}
    onlistentoggle={handleListenToggle}
  />
</div>

<style>
  .live-view {
    display: flex;
    flex-direction: column;
    height: 100%;
  }
  .scroll-area {
    flex: 1;
    overflow-y: auto;
    padding: 4px 0;
    position: relative;
  }
  .scroll-area::-webkit-scrollbar {
    width: 6px;
  }
  .scroll-area::-webkit-scrollbar-track {
    background: transparent;
  }
  .scroll-area::-webkit-scrollbar-thumb {
    background: rgba(255, 255, 255, 0.1);
    border-radius: 3px;
  }
  .scroll-area::-webkit-scrollbar-thumb:hover {
    background: rgba(255, 255, 255, 0.2);
  }
  .empty-state {
    text-align: center;
    color: #555;
    padding: 40px 20px;
    font-size: 14px;
  }
  .selection-toolbar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 6px 12px;
    background: rgba(74, 158, 255, 0.12);
    border-bottom: 1px solid rgba(74, 158, 255, 0.2);
    font-size: 13px;
    color: #ccc;
  }
  .selection-toolbar button {
    background: rgba(255, 255, 255, 0.08);
    border: 1px solid rgba(255, 255, 255, 0.15);
    color: #e0e0e0;
    padding: 3px 10px;
    border-radius: 3px;
    cursor: pointer;
    font-size: 12px;
  }
  .selection-toolbar button:hover {
    background: rgba(255, 255, 255, 0.15);
  }
  .selection-toolbar button.danger {
    border-color: rgba(231, 76, 60, 0.4);
    color: #e74c3c;
  }
  .selection-toolbar button.danger:hover {
    background: rgba(231, 76, 60, 0.15);
  }
</style>
