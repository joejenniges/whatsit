<script lang="ts">
  import { onMount } from 'svelte';

  // Current values
  let streamUrl = $state('');
  let asrEngine = $state('whisper');
  let modelSize = $state('base');
  let classifierTier = $state('whisper');
  let transcriptionMode = $state('segment');
  let useGpu = $state(true);
  let language = $state('en');
  let saveAudio = $state(false);
  let windowSize = $state(10);
  let windowStep = $state(3);
  let classifierDebug = $state(false);

  // Original values (from last save/load) for dirty detection.
  // Must be $state so $derived tracks changes to it.
  let original: Record<string, any> = $state({});

  let saving = $state(false);
  let saveMsg = $state('');
  let shakeSave = $state(false);
  let showRestartDialog = $state(false);

  // Fields that require app restart to take effect
  const restartFields = ['asrEngine', 'modelSize', 'classifierTier', 'transcriptionMode', 'useGpu'];

  function currentValues(): Record<string, any> {
    return { streamUrl, asrEngine, modelSize, classifierTier, transcriptionMode, useGpu, language, saveAudio, windowSize, windowStep, classifierDebug };
  }

  let dirty = $derived(JSON.stringify(currentValues()) !== JSON.stringify(original));

  function needsRestart(): boolean {
    const curr = currentValues();
    for (const f of restartFields) {
      if (curr[f] !== original[f]) return true;
    }
    return false;
  }

  // Expose a method for App.svelte to call before switching tabs
  export function canLeave(): boolean {
    if (!dirty) return true;
    shakeSave = true;
    setTimeout(() => shakeSave = false, 600);
    return false;
  }

  onMount(async () => {
    try {
      const { GetConfig } = await import('../../wailsjs/go/main/App');
      const cfg = await GetConfig();
      streamUrl = cfg.StreamURL || '';
      asrEngine = cfg.ASREngine || 'whisper';
      modelSize = cfg.ModelSize || 'base';
      classifierTier = cfg.ClassifierTier || 'whisper';
      transcriptionMode = cfg.TranscriptionMode || 'segment';
      useGpu = cfg.UseGPU ?? true;
      language = cfg.Language || 'en';
      saveAudio = cfg.SaveAudio || false;
      windowSize = cfg.WindowSizeSecs || 10;
      windowStep = cfg.WindowStepSecs || 3;
      classifierDebug = cfg.ClassifierDebug || false;
      original = currentValues();
    } catch {
      original = currentValues();
    }
  });

  async function handleSave() {
    saving = true;
    saveMsg = '';
    const restartNeeded = needsRestart();
    try {
      const { SaveConfig } = await import('../../wailsjs/go/main/App');
      await SaveConfig({
        StreamURL: streamUrl,
        ASREngine: asrEngine,
        ModelSize: modelSize,
        ModelPath: '',
        Language: language,
        BufferSecs: 0,
        ClassifierTier: classifierTier,
        TranscriptionMode: transcriptionMode,
        ClassifierDebug: classifierDebug,
        WindowSizeSecs: windowSize,
        WindowStepSecs: windowStep,
        SaveAudio: saveAudio,
        UseGPU: useGpu,
      });
      original = currentValues();
      if (restartNeeded) {
        showRestartDialog = true;
      } else {
        saveMsg = 'Saved';
        setTimeout(() => saveMsg = '', 2000);
      }
    } catch (e) {
      saveMsg = `Error: ${e}`;
    } finally {
      saving = false;
    }
  }

  async function handleRestart() {
    try {
      const { Quit } = await import('../../wailsjs/runtime/runtime');
      Quit();
    } catch {
      showRestartDialog = false;
    }
  }

  async function openLogsFolder() {
    try {
      const { OpenLogsFolder } = await import('../../wailsjs/go/main/App');
      await OpenLogsFolder();
    } catch { /* ignore */ }
  }

  async function openDataFolder() {
    try {
      const { OpenDataFolder } = await import('../../wailsjs/go/main/App');
      await OpenDataFolder();
    } catch { /* ignore */ }
  }
</script>

<div class="settings">
  <h2>Settings</h2>

  {#if showRestartDialog}
    <div class="restart-overlay">
      <div class="restart-dialog">
        <p>Settings saved. Some changes require a restart to take effect.</p>
        <div class="restart-actions">
          <button class="restart-btn" onclick={handleRestart}>Quit Now</button>
          <button class="later-btn" onclick={() => showRestartDialog = false}>Later</button>
        </div>
      </div>
    </div>
  {/if}

  <!-- Stream -->
  <section class="section">
    <h3>Stream</h3>
    <div class="form-group">
      <label for="stream-url">Stream URL</label>
      <input id="stream-url" type="text" bind:value={streamUrl} placeholder="https://..." />
    </div>
    <div class="form-group">
      <label for="language">Language</label>
      <select id="language" bind:value={language}>
        <option value="en">English</option>
        <option value="es">Spanish</option>
        <option value="fr">French</option>
        <option value="de">German</option>
        <option value="auto">Auto-detect</option>
      </select>
    </div>
  </section>

  <!-- Speech Recognition -->
  <section class="section">
    <h3>Speech Recognition</h3>
    <div class="form-row">
      <div class="form-group">
        <label for="asr-engine">ASR Engine (requires restart)</label>
        <select id="asr-engine" bind:value={asrEngine}>
          <option value="whisper">Whisper (whisper.cpp)</option>
          <option value="parakeet">Parakeet CTC (ONNX)</option>
        </select>
      </div>
      <div class="form-group">
        <label for="model-size">Whisper Model</label>
        <select id="model-size" bind:value={modelSize} disabled={asrEngine !== 'whisper'}>
          <option value="tiny">tiny (~75 MB)</option>
          <option value="base">base (~142 MB)</option>
          <option value="small">small (~466 MB)</option>
          <option value="medium">medium (~1.5 GB)</option>
        </select>
      </div>
    </div>
    <div class="form-group">
      <label for="transcription-mode">Transcription Mode (requires restart)</label>
      <select id="transcription-mode" bind:value={transcriptionMode}>
        <option value="segment">Segment -- transcribe on transition (cleaner text)</option>
        <option value="rolling">Rolling -- progressive output (lower latency)</option>
      </select>
    </div>
    <label class="check-label">
      <input type="checkbox" bind:checked={useGpu} />
      Use GPU acceleration (Vulkan, requires restart)
    </label>
  </section>

  <!-- Audio Classification -->
  <section class="section">
    <h3>Audio Classification</h3>
    <div class="form-group">
      <label for="classifier-tier">Classifier Tier (requires restart)</label>
      <select id="classifier-tier" bind:value={classifierTier}>
        <option value="basic">Basic (ZCR + spectral flatness)</option>
        <option value="scheirer">Scheirer (4-feature)</option>
        <option value="mfcc">MFCC (cepstral analysis)</option>
        <option value="whisper">Whisper (uses ASR)</option>
        <option value="whisper+rhythm">Whisper + Rhythm (recommended)</option>
        <option value="fusion">CED + Rhythm (AI classifier)</option>
        <option value="scheirer+rhythm">Scheirer + Rhythm</option>
        <option value="mfcc+rhythm">MFCC + Rhythm</option>
      </select>
    </div>
    <label class="check-label">
      <input type="checkbox" bind:checked={classifierDebug} />
      Log classifier feature values (debug)
    </label>
  </section>

  <!-- Advanced -->
  <section class="section">
    <h3>Advanced</h3>
    <p class="section-hint">Rolling window settings (only used in rolling transcription mode)</p>
    <div class="form-row">
      <div class="form-group">
        <label for="window-size">Window Size (seconds)</label>
        <input id="window-size" type="number" bind:value={windowSize} min="5" max="30" />
      </div>
      <div class="form-group">
        <label for="window-step">Window Step (seconds)</label>
        <input id="window-step" type="number" bind:value={windowStep} min="1" max="10" />
      </div>
    </div>
    <label class="check-label">
      <input type="checkbox" bind:checked={saveAudio} />
      Save audio segments to disk (WAV files in AppData)
    </label>
  </section>

  <div class="form-actions">
    {#if dirty}
      <button class="save-btn" class:shake={shakeSave} onclick={handleSave} disabled={saving}>
        {saving ? 'Saving...' : 'Save Changes'}
      </button>
    {/if}
    <button class="folder-btn" onclick={openLogsFolder}>Open Logs</button>
    <button class="folder-btn" onclick={openDataFolder}>Open Data Folder</button>
    {#if saveMsg}
      <span class="save-msg" class:error={saveMsg.startsWith('Error')}>{saveMsg}</span>
    {/if}
  </div>
</div>

<style>
  .settings {
    padding: 24px;
    max-width: 600px;
    margin: 0 auto;
    overflow-y: auto;
    height: 100%;
    position: relative;
  }
  h2 {
    color: #e0e0e0;
    font-size: 20px;
    margin: 0 0 24px 0;
    font-weight: 600;
  }
  .section {
    margin-bottom: 24px;
    padding: 16px;
    background: rgba(255, 255, 255, 0.02);
    border: 1px solid rgba(255, 255, 255, 0.06);
    border-radius: 8px;
  }
  h3 {
    color: #4a9eff;
    font-size: 13px;
    font-weight: 600;
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin: 0 0 12px 0;
  }
  .section-hint {
    color: #666;
    font-size: 11px;
    margin: 0 0 10px 0;
  }
  .form-group {
    margin-bottom: 12px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    flex: 1;
  }
  .form-group:last-child {
    margin-bottom: 0;
  }
  .form-row {
    display: flex;
    gap: 12px;
    margin-bottom: 12px;
  }
  .form-row:last-child {
    margin-bottom: 0;
  }
  label:not(.check-label) {
    color: #999;
    font-size: 12px;
    font-weight: 500;
  }
  input[type="text"],
  input[type="number"],
  select {
    background: rgba(255, 255, 255, 0.06);
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 4px;
    color: #e0e0e0;
    padding: 8px 10px;
    font-size: 13px;
    outline: none;
    width: 100%;
    box-sizing: border-box;
  }
  input:focus,
  select:focus {
    border-color: #4a9eff;
  }
  select {
    cursor: pointer;
  }
  select:disabled {
    opacity: 0.4;
    cursor: default;
  }
  select option {
    background: #16213e;
    color: #e0e0e0;
  }
  .check-label {
    display: flex;
    align-items: center;
    gap: 8px;
    color: #ccc;
    font-size: 13px;
    cursor: pointer;
    user-select: none;
    margin-top: 8px;
  }
  .form-actions {
    display: flex;
    align-items: center;
    gap: 12px;
    padding-top: 8px;
  }
  .save-btn {
    background: #4a9eff;
    border: none;
    border-radius: 6px;
    color: white;
    padding: 9px 28px;
    font-size: 14px;
    font-weight: 600;
    cursor: pointer;
  }
  .save-btn:hover:not(:disabled) {
    background: #3a8eef;
  }
  .save-btn:disabled {
    opacity: 0.5;
    cursor: default;
  }
  .save-btn.shake {
    animation: shake 0.5s ease-in-out;
  }
  @keyframes shake {
    0%, 100% { transform: translateX(0); }
    10%, 50%, 90% { transform: translateX(-4px); }
    30%, 70% { transform: translateX(4px); }
  }
  .folder-btn {
    background: rgba(255, 255, 255, 0.06);
    border: 1px solid rgba(255, 255, 255, 0.15);
    border-radius: 6px;
    color: #ccc;
    padding: 9px 16px;
    font-size: 13px;
    cursor: pointer;
  }
  .folder-btn:hover {
    background: rgba(255, 255, 255, 0.1);
    color: #fff;
  }
  .save-msg {
    font-size: 12px;
    color: #00c864;
  }
  .save-msg.error {
    color: #ff5050;
  }
  .restart-overlay {
    position: fixed;
    top: 0;
    left: 0;
    right: 0;
    bottom: 0;
    background: rgba(0, 0, 0, 0.6);
    display: flex;
    align-items: center;
    justify-content: center;
    z-index: 100;
  }
  .restart-dialog {
    background: #1e2a3a;
    border: 1px solid rgba(74, 158, 255, 0.3);
    border-radius: 12px;
    padding: 24px 32px;
    max-width: 400px;
    text-align: center;
  }
  .restart-dialog p {
    color: #e0e0e0;
    font-size: 14px;
    line-height: 1.5;
    margin: 0 0 20px 0;
  }
  .restart-actions {
    display: flex;
    gap: 12px;
    justify-content: center;
  }
  .restart-btn {
    background: #4a9eff;
    border: none;
    border-radius: 6px;
    color: white;
    padding: 8px 24px;
    font-size: 13px;
    font-weight: 600;
    cursor: pointer;
  }
  .restart-btn:hover {
    background: #3a8eef;
  }
  .later-btn {
    background: rgba(255, 255, 255, 0.06);
    border: 1px solid rgba(255, 255, 255, 0.15);
    border-radius: 6px;
    color: #ccc;
    padding: 8px 24px;
    font-size: 13px;
    cursor: pointer;
  }
  .later-btn:hover {
    background: rgba(255, 255, 255, 0.1);
  }
</style>
