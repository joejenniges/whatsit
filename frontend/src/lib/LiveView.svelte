<script lang="ts">
  import { onMount } from 'svelte';
  import SearchBar from './SearchBar.svelte';
  import TranscriptLine from './TranscriptLine.svelte';
  import SongLine from './SongLine.svelte';
  import InsertZone from './InsertZone.svelte';
  import ScrollToBottom from './ScrollToBottom.svelte';
  import StatusBar from './StatusBar.svelte';
  import {
    getEntries, appendEntry, updateEntry, insertAfter,
    setEntries, fromLogEntry, subscribe as entriesSub,
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

  // Reactive state driven by stores
  let entries: Entry[] = $state([]);
  let query = $state('');
  let filterActive = $state(false);
  let regex: RegExp | null = $state(null);
  let connected = $state(false);
  let classification = $state('--');
  let streaming = $state(false);
  let listenEnabled = $state(false);

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
    entries = getEntries();
    query = getQuery();
    filterActive = getFilterActive();
    regex = getRegex();
    connected = getConnected();
    classification = getClassification();
    streaming = getStreaming();
    listenEnabled = getListenEnabled();
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

<div class="live-view">
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
          timestamp={entry.timestamp}
          content={entry.content}
          {regex}
        />
      {:else}
        <SongLine
          id={entry.id}
          title={entry.title}
          artist={entry.artist}
          content={entry.content}
          entryType={entry.type}
          {regex}
          onsave={handleSongSave}
        />
      {/if}
    {/each}

    {#if entries.length === 0}
      <div class="empty-state">No entries yet. Start streaming to see transcriptions.</div>
    {/if}

    {#if filterActive && regex && visibleEntries.length === 0 && entries.length > 0}
      <div class="empty-state">No matching entries.</div>
    {/if}

    <ScrollToBottom visible={showScrollBtn} onclick={handleScrollToBottom} />
  </div>

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
</style>
