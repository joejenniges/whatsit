<script lang="ts">
  import { onMount } from 'svelte';

  let entries: any[] = $state([]);
  let config: any = $state({});
  let status = $state('Loading...');

  onMount(async () => {
    try {
      // These imports will resolve once wails generates the bindings
      const { GetConfig, GetRecentEntries } = await import('../wailsjs/go/main/App');
      config = await GetConfig();
      const recent = await GetRecentEntries(20);
      entries = recent || [];
      status = `Loaded ${entries.length} entries. Stream URL: ${config.stream_url || '(not set)'}`;
    } catch (e) {
      status = `Error: ${e}`;
    }
  });
</script>

<main>
  <h1>RadioTranscriber</h1>
  <p class="status">{status}</p>

  <div class="entries">
    {#each entries as entry}
      <div class="entry {entry.EntryType}">
        <span class="timestamp">[{new Date(entry.Timestamp).toLocaleTimeString()}]</span>
        <span class="content">{entry.Content || entry.EntryType}</span>
      </div>
    {/each}
  </div>
</main>

<style>
  main {
    font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
    padding: 16px;
    background: #1a1a2e;
    color: #e0e0e0;
    min-height: 100vh;
  }

  h1 {
    color: #4a9eff;
    margin-bottom: 8px;
    font-size: 20px;
  }

  .status {
    color: #888;
    font-size: 12px;
    margin-bottom: 16px;
  }

  .entries {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }

  .entry {
    padding: 4px 8px;
    border-radius: 4px;
    font-size: 14px;
    line-height: 1.4;
  }

  .entry.speech {
    background: rgba(255, 255, 255, 0.05);
  }

  .entry.song, .entry.music_unknown {
    background: rgba(74, 158, 255, 0.1);
    font-weight: bold;
  }

  .timestamp {
    color: #666;
    margin-right: 8px;
    font-family: monospace;
    font-size: 12px;
  }
</style>
