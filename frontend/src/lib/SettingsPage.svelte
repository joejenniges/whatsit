<script lang="ts">
  import { onMount } from 'svelte';

  let streamUrl = $state('');
  let asrEngine = $state('whisper');
  let modelSize = $state('base');
  let classifierTier = $state('basic');
  let transcriptionMode = $state('segment');
  let useGpu = $state(false);
  let acoustIdKey = $state('');
  let language = $state('en');
  let saveAudio = $state(false);
  let windowSize = $state(5);
  let windowStep = $state(3);
  let classifierDebug = $state(false);
  let saving = $state(false);
  let saveMsg = $state('');

  onMount(async () => {
    try {
      const { GetConfig } = await import('../../wailsjs/go/main/App');
      const cfg = await GetConfig();
      streamUrl = cfg.StreamURL || '';
      asrEngine = cfg.ASREngine || 'whisper';
      modelSize = cfg.ModelSize || 'base';
      classifierTier = cfg.ClassifierTier || 'basic';
      transcriptionMode = cfg.TranscriptionMode || 'segment';
      useGpu = cfg.UseGPU || false;
      acoustIdKey = cfg.AcoustIDKey || '';
      language = cfg.Language || 'en';
      saveAudio = cfg.SaveAudio || false;
      windowSize = cfg.WindowSizeSecs || 5;
      windowStep = cfg.WindowStepSecs || 3;
      classifierDebug = cfg.ClassifierDebug || false;
    } catch {
      // Bindings not available yet
    }
  });

  async function handleSave() {
    saving = true;
    saveMsg = '';
    try {
      const { SaveConfig } = await import('../../wailsjs/go/main/App');
      await SaveConfig({
        StreamURL: streamUrl,
        ASREngine: asrEngine,
        ModelSize: modelSize,
        ModelPath: '',
        AcoustIDKey: acoustIdKey,
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
      saveMsg = 'Saved';
      setTimeout(() => saveMsg = '', 2000);
    } catch (e) {
      saveMsg = `Error: ${e}`;
    } finally {
      saving = false;
    }
  }
</script>

<div class="settings">
  <h2>Settings</h2>

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
        <label for="asr-engine">ASR Engine</label>
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
      <label for="transcription-mode">Transcription Mode</label>
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
      <label for="classifier-tier">Classifier Tier</label>
      <select id="classifier-tier" bind:value={classifierTier}>
        <option value="basic">Basic (ZCR + spectral flatness)</option>
        <option value="scheirer">Scheirer (4-feature, recommended)</option>
        <option value="mfcc">MFCC (cepstral analysis)</option>
        <option value="whisper">Whisper (uses ASR for classification)</option>
      </select>
    </div>
    <label class="check-label">
      <input type="checkbox" bind:checked={classifierDebug} />
      Log classifier feature values (debug)
    </label>
  </section>

  <!-- Song Identification -->
  <section class="section">
    <h3>Song Identification</h3>
    <div class="form-group">
      <label for="acoustid-key">AcoustID API Key</label>
      <input id="acoustid-key" type="text" bind:value={acoustIdKey} placeholder="Get a free key at acoustid.org" />
    </div>
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
    <button class="save-btn" onclick={handleSave} disabled={saving}>
      {saving ? 'Saving...' : 'Save'}
    </button>
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
  .save-msg {
    font-size: 12px;
    color: #00c864;
  }
  .save-msg.error {
    color: #ff5050;
  }
</style>
