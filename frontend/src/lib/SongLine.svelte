<script lang="ts">
  import { splitAtMatches, type TextSegment } from '../utils/highlight';

  let { id, title, artist, content, entryType, timestamp, regex, onsave, ondelete }: {
    id: number;
    title: string;
    artist: string;
    content: string;
    entryType: 'song' | 'music_unknown';
    timestamp: Date;
    regex: RegExp | null;
    onsave: (id: number, title: string, artist: string) => void;
    ondelete: () => void;
  } = $props();

  let timeStr = $derived(
    timestamp.toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
  );

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

  let editing = $state(false);
  let editTitle = $state('');
  let editArtist = $state('');

  let displayText = $derived(() => {
    if (entryType === 'music_unknown' && !title && !artist) {
      return 'Song played';
    }
    if (title && artist) return `"${title}" - ${artist}`;
    if (title) return `"${title}"`;
    if (artist) return artist;
    return content || 'Song played';
  });

  let segments: TextSegment[] = $derived(splitAtMatches(displayText(), regex));

  function startEdit() {
    editTitle = title;
    editArtist = artist;
    editing = true;
  }

  function save() {
    onsave(id, editTitle.trim(), editArtist.trim());
    editing = false;
  }

  function cancel() {
    editing = false;
  }

  function handleKeydown(e: KeyboardEvent) {
    if (e.key === 'Enter') save();
    if (e.key === 'Escape') cancel();
  }
</script>

<div class="entry song">
  {#if editing}
    <div class="edit-form">
      <input
        class="edit-input"
        type="text"
        placeholder="Title"
        bind:value={editTitle}
        onkeydown={handleKeydown}
      />
      <span class="edit-sep">-</span>
      <input
        class="edit-input"
        type="text"
        placeholder="Artist"
        bind:value={editArtist}
        onkeydown={handleKeydown}
      />
      <button class="edit-ok" onclick={save}>OK</button>
      <button class="edit-cancel" onclick={cancel}>Cancel</button>
    </div>
  {:else}
    <span class="timestamp">[{timeStr}]</span>
    <span class="music-icon">&#9835;</span>
    <span class="song-text">
      {#each segments as seg}
        {#if seg.matched}
          <mark class="search-match">{seg.text}</mark>
        {:else}
          {seg.text}
        {/if}
      {/each}
    </span>
    <button class="edit-btn" onclick={startEdit} title="Edit song info">&#9998;</button>
    <button class="delete-btn" class:confirm={confirmDelete} onclick={handleDelete}>
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
  }
  .song {
    background: rgba(74, 158, 255, 0.15);
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .timestamp {
    color: #666;
    margin-right: 8px;
    font-family: monospace;
    font-size: 12px;
    user-select: none;
    flex-shrink: 0;
  }
  .music-icon {
    color: #4a9eff;
    margin-right: 6px;
    font-size: 16px;
    flex-shrink: 0;
  }
  .song-text {
    font-weight: 600;
    color: #4a9eff;
    flex: 1;
  }
  .edit-btn {
    background: none;
    border: none;
    color: #666;
    cursor: pointer;
    font-size: 14px;
    padding: 2px 6px;
    border-radius: 3px;
    opacity: 0;
    transition: opacity 0.15s;
    flex-shrink: 0;
  }
  .song:hover .edit-btn {
    opacity: 1;
  }
  .edit-btn:hover {
    color: #4a9eff;
    background: rgba(74, 158, 255, 0.1);
  }
  .edit-form {
    display: flex;
    align-items: center;
    gap: 6px;
    flex: 1;
  }
  .edit-input {
    background: rgba(255, 255, 255, 0.08);
    border: 1px solid rgba(74, 158, 255, 0.3);
    border-radius: 3px;
    color: #e0e0e0;
    padding: 3px 6px;
    font-size: 13px;
    flex: 1;
    outline: none;
  }
  .edit-input:focus {
    border-color: #4a9eff;
  }
  .edit-sep {
    color: #666;
  }
  .edit-ok, .edit-cancel {
    background: none;
    border: 1px solid rgba(255, 255, 255, 0.15);
    color: #e0e0e0;
    padding: 3px 8px;
    border-radius: 3px;
    cursor: pointer;
    font-size: 12px;
  }
  .edit-ok:hover {
    background: rgba(74, 158, 255, 0.2);
    border-color: #4a9eff;
  }
  .edit-cancel:hover {
    background: rgba(255, 255, 255, 0.05);
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
    transition: opacity 0.15s;
  }
  .song:hover .delete-btn {
    opacity: 1;
  }
  .delete-btn.confirm {
    opacity: 1;
    border-color: #e74c3c;
    color: #e74c3c;
  }
</style>
