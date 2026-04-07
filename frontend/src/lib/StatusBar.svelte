<script lang="ts">
  let { connected, classification, classifierTier, whisperLoad, streaming, listenEnabled, onstart, onstop, onlistentoggle }: {
    connected: boolean;
    classification: string;
    classifierTier: string;
    whisperLoad: number;
    streaming: boolean;
    listenEnabled: boolean;
    onstart: () => void;
    onstop: () => void;
    onlistentoggle: (enabled: boolean) => void;
  } = $props();

  let loadPercent = $derived(Math.round(whisperLoad * 100));
  let loadClass = $derived(
    whisperLoad > 1.0 ? 'overloaded' : whisperLoad > 0.7 ? 'high' : 'normal'
  );
</script>

<div class="status-bar">
  <div class="status-info">
    <span class="connection" class:connected class:disconnected={!connected}>
      {connected ? 'Connected' : 'Disconnected'}
    </span>
    <span class="classification">Classification: {classification}</span>
    {#if classifierTier}
      <span class="tier">Classifier: {classifierTier}</span>
    {/if}
    {#if whisperLoad > 0}
      <span class="load {loadClass}">
        Whisper: {loadPercent}%
      </span>
    {/if}
  </div>
  <div class="controls">
    <button class="ctrl-btn start" onclick={onstart} disabled={streaming}>Start</button>
    <button class="ctrl-btn stop" onclick={onstop} disabled={!streaming}>Stop</button>
    <label class="listen-label">
      <input
        type="checkbox"
        checked={listenEnabled}
        onchange={(e) => onlistentoggle((e.target as HTMLInputElement).checked)}
      />
      Listen
    </label>
  </div>
</div>

<style>
  .status-bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 6px 12px;
    border-top: 1px solid rgba(255, 255, 255, 0.08);
    background: var(--color-surface);
    font-size: 12px;
    flex-shrink: 0;
  }
  .status-info {
    display: flex;
    align-items: center;
    gap: 16px;
  }
  .connection {
    padding: 2px 8px;
    border-radius: 3px;
    font-weight: 600;
    font-size: 11px;
    text-transform: uppercase;
    letter-spacing: 0.3px;
  }
  .connected {
    background: rgba(0, 200, 100, 0.15);
    color: #00c864;
  }
  .disconnected {
    background: rgba(255, 80, 80, 0.15);
    color: #ff5050;
  }
  .classification {
    color: #888;
  }
  .tier {
    color: #666;
    font-style: italic;
  }
  .load {
    padding: 1px 6px;
    border-radius: 3px;
    font-weight: 500;
  }
  .load.normal {
    color: #00c864;
  }
  .load.high {
    color: #ffaa00;
    background: rgba(255, 170, 0, 0.1);
  }
  .load.overloaded {
    color: #ff5050;
    background: rgba(255, 80, 80, 0.15);
    font-weight: 700;
  }
  .controls {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .ctrl-btn {
    padding: 3px 12px;
    border-radius: 3px;
    border: 1px solid rgba(255, 255, 255, 0.15);
    background: rgba(255, 255, 255, 0.06);
    color: #e0e0e0;
    font-size: 12px;
    cursor: pointer;
  }
  .ctrl-btn:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .ctrl-btn.start:not(:disabled):hover {
    background: rgba(0, 200, 100, 0.15);
    border-color: #00c864;
    color: #00c864;
  }
  .ctrl-btn.stop:not(:disabled):hover {
    background: rgba(255, 80, 80, 0.15);
    border-color: #ff5050;
    color: #ff5050;
  }
  .listen-label {
    display: flex;
    align-items: center;
    gap: 4px;
    color: #999;
    cursor: pointer;
    user-select: none;
  }
  .listen-label input {
    cursor: pointer;
  }
</style>
