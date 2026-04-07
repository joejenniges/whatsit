<script lang="ts">
  let { connected, classification, classifierTier, whisperLoad, cedLoadMs,
        cedSpeech, cedMusic, cedSinging, cedTop, cedTopScore, cedGenre,
        streaming, listenEnabled, onstart, onstop, onlistentoggle }: {
    connected: boolean;
    classification: string;
    classifierTier: string;
    whisperLoad: number;
    cedLoadMs: number;
    cedSpeech: boolean;
    cedMusic: boolean;
    cedSinging: boolean;
    cedTop: string;
    cedTopScore: number;
    cedGenre: string;
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

  let cedFlags = $derived(
    [cedSpeech && 'S', cedMusic && 'M', cedSinging && 'V'].filter(Boolean).join('')
  );
</script>

<div class="status-bar">
  <div class="status-row top-row">
    <div class="status-info">
      <span class="connection" class:connected class:disconnected={!connected}>
        {connected ? 'Connected' : 'Disconnected'}
      </span>
      <span class="classification">Class: {classification}</span>
      {#if classifierTier}
        <span class="tier">{classifierTier}</span>
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

  {#if cedTop || whisperLoad > 0}
    <div class="status-row detail-row">
      {#if cedTop}
        <span class="ced-detail">
          CED: <strong>{cedTop}</strong> ({(cedTopScore * 100).toFixed(0)}%)
          <span class="ced-flags">[{cedFlags}]</span>
          {#if cedGenre}
            <span class="ced-genre">{cedGenre}</span>
          {/if}
          <span class="ced-time">{cedLoadMs.toFixed(0)}ms</span>
        </span>
      {/if}
      {#if whisperLoad > 0}
        <span class="load {loadClass}">Whisper: {loadPercent}%</span>
      {/if}
    </div>
  {/if}
</div>

<style>
  .status-bar {
    border-top: 1px solid rgba(255, 255, 255, 0.08);
    background: var(--color-surface);
    font-size: 12px;
    flex-shrink: 0;
  }
  .status-row {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 4px 12px;
  }
  .detail-row {
    padding-top: 0;
    gap: 16px;
  }
  .status-info {
    display: flex;
    align-items: center;
    gap: 12px;
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
    color: #555;
    font-style: italic;
  }
  .ced-detail {
    color: #999;
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .ced-detail strong {
    color: #ccc;
  }
  .ced-flags {
    color: #4a9eff;
    font-family: monospace;
    font-size: 11px;
  }
  .ced-genre {
    color: #ffaa00;
    font-weight: 500;
  }
  .ced-time {
    color: #666;
    font-size: 11px;
  }
  .load {
    padding: 1px 6px;
    border-radius: 3px;
    font-weight: 500;
  }
  .load.normal { color: #00c864; }
  .load.high { color: #ffaa00; background: rgba(255, 170, 0, 0.1); }
  .load.overloaded { color: #ff5050; background: rgba(255, 80, 80, 0.15); font-weight: 700; }
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
  .ctrl-btn:disabled { opacity: 0.4; cursor: default; }
  .ctrl-btn.start:not(:disabled):hover { background: rgba(0, 200, 100, 0.15); border-color: #00c864; color: #00c864; }
  .ctrl-btn.stop:not(:disabled):hover { background: rgba(255, 80, 80, 0.15); border-color: #ff5050; color: #ff5050; }
  .listen-label {
    display: flex;
    align-items: center;
    gap: 4px;
    color: #999;
    cursor: pointer;
    user-select: none;
  }
  .listen-label input { cursor: pointer; }
</style>
