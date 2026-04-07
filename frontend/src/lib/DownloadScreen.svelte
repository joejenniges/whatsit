<script lang="ts">
  let { modelSize, percent, speed, eta }: {
    modelSize: string;
    percent: number;
    speed: string;
    eta: string;
  } = $props();

  async function handleCancel() {
    try {
      const { StopStreaming } = await import('../../wailsjs/go/main/App');
      await StopStreaming();
    } catch {
      // Binding not available
    }
  }
</script>

<div class="download-screen">
  <div class="download-card">
    <h2>Downloading Models</h2>
    <p class="model-info">{modelSize || 'base'} model</p>

    <div class="progress-container">
      <div class="progress-bar">
        <div class="progress-fill" style="width: {percent}%"></div>
      </div>
      <span class="progress-percent">{percent.toFixed(0)}%</span>
    </div>

    <div class="progress-details">
      {#if speed}
        <span>{speed}</span>
      {/if}
      {#if eta}
        <span>ETA: {eta}</span>
      {/if}
    </div>

    <button class="cancel-btn" onclick={handleCancel}>Cancel</button>
  </div>
</div>

<style>
  .download-screen {
    display: flex;
    align-items: center;
    justify-content: center;
    height: 100%;
    padding: 20px;
  }
  .download-card {
    text-align: center;
    max-width: 400px;
    width: 100%;
  }
  h2 {
    color: #e0e0e0;
    font-size: 18px;
    margin: 0 0 8px 0;
    font-weight: 600;
  }
  .model-info {
    color: #888;
    font-size: 13px;
    margin: 0 0 24px 0;
  }
  .progress-container {
    display: flex;
    align-items: center;
    gap: 10px;
    margin-bottom: 10px;
  }
  .progress-bar {
    flex: 1;
    height: 8px;
    background: rgba(255, 255, 255, 0.08);
    border-radius: 4px;
    overflow: hidden;
  }
  .progress-fill {
    height: 100%;
    background: #4a9eff;
    border-radius: 4px;
    transition: width 0.3s ease;
  }
  .progress-percent {
    color: #e0e0e0;
    font-size: 13px;
    font-weight: 600;
    min-width: 36px;
    text-align: right;
  }
  .progress-details {
    display: flex;
    justify-content: center;
    gap: 16px;
    color: #888;
    font-size: 12px;
    margin-bottom: 20px;
  }
  .cancel-btn {
    background: rgba(255, 255, 255, 0.06);
    border: 1px solid rgba(255, 255, 255, 0.15);
    border-radius: 4px;
    color: #999;
    padding: 6px 20px;
    font-size: 13px;
    cursor: pointer;
  }
  .cancel-btn:hover {
    background: rgba(255, 80, 80, 0.15);
    border-color: #ff5050;
    color: #ff5050;
  }
</style>
