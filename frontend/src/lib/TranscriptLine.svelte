<script lang="ts">
  import { splitAtMatches, type TextSegment } from '../utils/highlight';

  let { timestamp, content, regex, ondelete }: {
    timestamp: Date;
    content: string;
    regex: RegExp | null;
    ondelete: () => void;
  } = $props();

  let timeStr = $derived(
    timestamp.toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
  );

  let segments: TextSegment[] = $derived(splitAtMatches(content, regex));
  let confirmDelete = $state(false);

  function handleDelete() {
    if (confirmDelete) {
      ondelete();
      confirmDelete = false;
    } else {
      confirmDelete = true;
      setTimeout(() => { confirmDelete = false; }, 3000);
    }
  }
</script>

<div class="entry speech">
  <span class="timestamp">[{timeStr}]</span>
  <span class="content">
    {#each segments as seg}
      {#if seg.matched}
        <mark class="search-match">{seg.text}</mark>
      {:else}
        {seg.text}
      {/if}
    {/each}
  </span>
  <button class="delete-btn" class:confirm={confirmDelete} onclick={handleDelete}>
    {confirmDelete ? 'Confirm?' : 'x'}
  </button>
</div>

<style>
  .entry {
    padding: 4px 8px;
    border-radius: 4px;
    font-size: 14px;
    line-height: 1.5;
    display: flex;
    align-items: baseline;
    position: relative;
  }
  .speech {
    background: rgba(255, 255, 255, 0.03);
  }
  .timestamp {
    color: #666;
    margin-right: 8px;
    font-family: monospace;
    font-size: 12px;
    user-select: none;
    flex-shrink: 0;
  }
  .content {
    word-break: break-word;
    flex: 1;
  }
  .delete-btn {
    opacity: 0;
    background: none;
    border: 1px solid #555;
    color: #888;
    font-size: 11px;
    padding: 1px 6px;
    border-radius: 3px;
    cursor: pointer;
    flex-shrink: 0;
    margin-left: 8px;
    transition: opacity 0.15s;
  }
  .entry:hover .delete-btn {
    opacity: 1;
  }
  .delete-btn.confirm {
    opacity: 1;
    border-color: #e74c3c;
    color: #e74c3c;
  }
  :global(.search-match) {
    background: rgba(255, 200, 0, 0.3);
    color: inherit;
    border-radius: 2px;
    padding: 0 1px;
  }
</style>
