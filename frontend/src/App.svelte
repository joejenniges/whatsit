<script lang="ts">
  import { onMount } from 'svelte';
  import LiveView from './lib/LiveView.svelte';
  import SettingsPage from './lib/SettingsPage.svelte';
  import DownloadScreen from './lib/DownloadScreen.svelte';
  import GpuWarning from './lib/GpuWarning.svelte';
  import { init, getDownload, getGPUWarning, subscribe } from './store';

  type Tab = 'live' | 'settings';
  let currentTab: Tab = $state('live');
  let download = $state({ active: false, percent: 0, message: '' });
  let gpuWarning = $state('');
  let settingsRef: SettingsPage | undefined = $state(undefined);

  function switchTab(tab: Tab) {
    if (currentTab === 'settings' && tab !== 'settings' && settingsRef) {
      if (!settingsRef.canLeave()) return;
    }
    currentTab = tab;
  }

  onMount(() => {
    init();
    return subscribe(() => {
      download = getDownload();
      gpuWarning = getGPUWarning();
    });
  });
</script>

<div class="app">
  {#if gpuWarning}
    <GpuWarning message={gpuWarning} />
  {/if}

  {#if download.active}
    <div class="view-container">
      <DownloadScreen
        modelSize=""
        percent={download.percent}
        speed=""
        eta={download.message}
      />
    </div>
  {:else}
    <nav class="tab-bar">
      <button
        class="tab"
        class:active={currentTab === 'live'}
        onclick={() => switchTab('live')}
      >Live</button>
      <button
        class="tab"
        class:active={currentTab === 'settings'}
        onclick={() => switchTab('settings')}
      >Settings</button>
    </nav>

    <div class="view-container">
      {#if currentTab === 'live'}
        <LiveView />
      {:else}
        <SettingsPage bind:this={settingsRef} />
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
