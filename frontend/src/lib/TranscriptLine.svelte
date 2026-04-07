<script lang="ts">
  import { splitAtMatches, type TextSegment } from '../utils/highlight';

  let { id, timestamp, content, regex, ondelete, onedit, selected, ontoggleselect, fresh = false }: {
    id: number;
    timestamp: Date;
    content: string;
    regex: RegExp | null;
    ondelete: () => void;
    onedit: (id: number, content: string) => void;
    selected: boolean;
    ontoggleselect: (id: number, shiftKey: boolean) => void;
    fresh?: boolean;
  } = $props();

  let timeStr = $derived(formatTimestamp(timestamp));

  function formatTimestamp(d: Date): string {
    const months = ['Jan','Feb','Mar','Apr','May','Jun','Jul','Aug','Sep','Oct','Nov','Dec'];
    const mon = months[d.getMonth()];
    const day = d.getDate();
    const yr = String(d.getFullYear()).slice(-2);
    const time = d.toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' });
    return `${mon} ${day} ${yr}, ${time}`;
  }

  let segments: TextSegment[] = $derived(splitAtMatches(content, regex));
  let confirmDelete = $state(false);
  let editing = $state(false);
  let editText = $state('');

  function handleDelete() {
    if (confirmDelete) {
      ondelete();
      confirmDelete = false;
    } else {
      confirmDelete = true;
      setTimeout(() => { confirmDelete = false; }, 3000);
    }
  }

  function startEdit() {
    editText = content;
    editing = true;
  }

  function saveEdit() {
    const trimmed = editText.trim();
    if (trimmed && trimmed !== content) {
      onedit(id, trimmed);
    }
    editing = false;
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') saveEdit();
    if (e.key === 'Escape') editing = false;
  }

  function handleClick(e: MouseEvent) {
    if (e.shiftKey || e.ctrlKey || e.metaKey) {
      e.preventDefault();
      ontoggleselect(id, e.shiftKey);
    }
  }
</script>

<div
  class="entry speech"
  class:selected
  class:fresh
  onclick={handleClick}
  role="listitem"
>
  {#if editing}
    <span class="timestamp">[{timeStr}]</span>
    <input
      class="edit-input"
      type="text"
      bind:value={editText}
      onblur={saveEdit}
      onkeydown={handleKeydown}
    />
  {:else}
    <span class="timestamp">[{timeStr}]</span>
    <span class="content" ondblclick={startEdit}>
      {#each segments as seg}
        {#if seg.matched}
          <mark class="search-match">{seg.text}</mark>
        {:else}
          {seg.text}
        {/if}
      {/each}
    </span>
    <button class="delete-btn" class:confirm={confirmDelete} onclick={(e) => { e.stopPropagation(); handleDelete(); }}>
      {confirmDelete ? 'Confirm?' : 'x'}
    </button>
  {/if}
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
    cursor: default;
    user-select: text;
  }
  .speech {
    background: rgba(255, 255, 255, 0.03);
  }
  .speech.fresh {
    animation: highlight-new 3s ease-out;
  }
  @keyframes highlight-new {
    0% { background: rgba(74, 158, 255, 0.2); }
    100% { background: rgba(255, 255, 255, 0.03); }
  }
  .speech.selected {
    background: rgba(74, 158, 255, 0.15);
    outline: 1px solid rgba(74, 158, 255, 0.3);
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
  .edit-input {
    flex: 1;
    background: rgba(255, 255, 255, 0.08);
    border: 1px solid rgba(74, 158, 255, 0.3);
    border-radius: 3px;
    color: #e0e0e0;
    padding: 3px 6px;
    font-size: 14px;
    outline: none;
  }
  .edit-input:focus {
    border-color: #4a9eff;
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
