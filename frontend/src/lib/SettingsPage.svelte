<script lang="ts">
  import { onMount } from 'svelte';

  let streamUrl = $state('');
  let modelSize = $state('base');
  let classifierTier = $state('basic');
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
      modelSize = cfg.ModelSize || 'base';
      classifierTier = cfg.ClassifierTier || 'basic';
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
        ModelSize: modelSize,
        ModelPath: '',
        AcoustIDKey: acoustIdKey,
        Language: language,
        BufferSecs: 0,
        ClassifierTier: classifierTier,
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

  <div class="form-group">
    <label for="stream-url">Stream URL</label>
    <input id="stream-url" type="text" bind:value={streamUrl} placeholder="http://..." />
  </div>

  <div class="form-row">
    <div class="form-group">
      <label for="model-size">Whisper Model</label>
      <select id="model-size" bind:value={modelSize}>
        <option value="tiny">tiny</option>
        <option value="base">base</option>
        <option value="small">small</option>
        <option value="medium">medium</option>
      </select>
    </div>

    <div class="form-group">
      <label for="classifier-tier">Classifier Tier</label>
      <select id="classifier-tier" bind:value={classifierTier}>
        <option value="basic">basic</option>
        <option value="scheirer">scheirer</option>
        <option value="mfcc">mfcc</option>
        <option value="whisper">whisper</option>
      </select>
    </div>
  </div>

  <div class="form-group">
    <label for="acoustid-key">AcoustID API Key</label>
    <input id="acoustid-key" type="text" bind:value={acoustIdKey} placeholder="API key" />
  </div>

  <div class="form-row">
    <div class="form-group">
      <label for="language">Language</label>
      <select id="language" bind:value={language}>
        <option value="en">English</option>
        <option value="es">Spanish</option>
        <option value="fr">French</option>
        <option value="de">German</option>
        <option value="auto">Auto</option>
      </select>
    </div>

    <div class="form-group">
      <label for="window-size">Window Size (s)</label>
      <input id="window-size" type="number" bind:value={windowSize} min="1" max="30" />
    </div>

    <div class="form-group">
      <label for="window-step">Window Step (s)</label>
      <input id="window-step" type="number" bind:value={windowStep} min="1" max="30" />
    </div>
  </div>

  <div class="form-checks">
    <label class="check-label">
      <input type="checkbox" bind:checked={useGpu} />
      Use GPU
    </label>
    <label class="check-label">
      <input type="checkbox" bind:checked={saveAudio} />
      Save Audio
    </label>
    <label class="check-label">
      <input type="checkbox" bind:checked={classifierDebug} />
      Classifier Debug
    </label>
  </div>

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
    padding: 20px;
    max-width: 560px;
    margin: 0 auto;
  }
  h2 {
    color: #e0e0e0;
    font-size: 18px;
    margin: 0 0 20px 0;
    font-weight: 600;
  }
  .form-group {
    margin-bottom: 14px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    flex: 1;
  }
  .form-row {
    display: flex;
    gap: 12px;
  }
  label {
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
    padding: 6px 8px;
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
  select option {
    background: #16213e;
    color: #e0e0e0;
  }
  .form-checks {
    display: flex;
    gap: 20px;
    margin-bottom: 20px;
    flex-wrap: wrap;
  }
  .check-label {
    display: flex;
    align-items: center;
    gap: 6px;
    color: #ccc;
    font-size: 13px;
    cursor: pointer;
    user-select: none;
  }
  .form-actions {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .save-btn {
    background: #4a9eff;
    border: none;
    border-radius: 4px;
    color: white;
    padding: 7px 24px;
    font-size: 13px;
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
