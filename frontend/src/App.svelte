<script lang="ts">
  import { onMount } from 'svelte';
  import LiveView from './lib/LiveView.svelte';
  import SettingsPage from './lib/SettingsPage.svelte';
  import DownloadScreen from './lib/DownloadScreen.svelte';
  import GpuWarning from './lib/GpuWarning.svelte';

  type View = 'main' | 'download';
  type Tab = 'live' | 'settings';

  let currentView: View = $state('main');
  let currentTab: Tab = $state('live');
  let gpuWarning = $state('');
  let downloadModelSize = $state('');

  // Download progress state managed here since DownloadScreen
  // can't easily set up its own event listeners before mount
  let downloadPercent = $state(0);
  let downloadSpeed = $state('');
  let downloadEta = $state('');

  onMount(() => {
    setupEvents();
  });

  async function setupEvents() {
    try {
      const { EventsOn } = await import('../wailsjs/runtime/runtime');

      EventsOn('show-main', () => {
        currentView = 'main';
      });

      EventsOn('show-download', (data: any) => {
        currentView = 'download';
        downloadModelSize = data?.modelSize || '';
      });

      EventsOn('gpu-warning', (message: string) => {
        gpuWarning = message;
      });

      EventsOn('download-progress', (data: any) => {
        downloadPercent = data.percent || 0;
        downloadSpeed = data.speed || '';
        downloadEta = data.eta || '';
      });
    } catch {
      // Runtime not available in dev mode
    }
  }
</script>

<div class="app">
  {#if gpuWarning}
    <GpuWarning message={gpuWarning} />
  {/if}

  {#if currentView === 'download'}
    <div class="view-container">
      <DownloadScreen
        modelSize={downloadModelSize}
        percent={downloadPercent}
        speed={downloadSpeed}
        eta={downloadEta}
      />
    </div>
  {:else}
    <nav class="tab-bar">
      <button
        class="tab"
        class:active={currentTab === 'live'}
        onclick={() => currentTab = 'live'}
      >Live</button>
      <button
        class="tab"
        class:active={currentTab === 'settings'}
        onclick={() => currentTab = 'settings'}
      >Settings</button>
    </nav>

    <div class="view-container">
      {#if currentTab === 'live'}
        <LiveView />
      {:else}
        <SettingsPage />
      {/if}
    </div>
  {/if}
</div>

<style>
  .app {
    display: flex;
    flex-direction: column;
    height: 100vh;
    overflow: hidden;
  }
  .tab-bar {
    display: flex;
    border-bottom: 1px solid rgba(255, 255, 255, 0.08);
    background: var(--color-surface);
    flex-shrink: 0;
  }
  .tab {
    padding: 8px 20px;
    background: none;
    border: none;
    border-bottom: 2px solid transparent;
    color: #777;
    font-size: 13px;
    font-weight: 500;
    cursor: pointer;
    transition: color 0.15s, border-color 0.15s;
  }
  .tab:hover {
    color: #bbb;
  }
  .tab.active {
    color: #4a9eff;
    border-bottom-color: #4a9eff;
  }
  .view-container {
    flex: 1;
    overflow: hidden;
  }
</style>
