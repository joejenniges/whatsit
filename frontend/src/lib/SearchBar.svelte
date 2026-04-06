<script lang="ts">
  let { query, filterActive, onquery, ontogglefilter, onclear, oninsertsong }: {
    query: string;
    filterActive: boolean;
    onquery: (q: string) => void;
    ontogglefilter: () => void;
    onclear: () => void;
    oninsertsong: () => void;
  } = $props();

  function handleInput(e: Event) {
    const target = e.target as HTMLInputElement;
    onquery(target.value);
  }
</script>

<div class="search-bar">
  <div class="search-group">
    <input
      class="search-input"
      type="text"
      placeholder="Search (* = wildcard)..."
      value={query}
      oninput={handleInput}
    />
    <button
      class="filter-btn"
      class:active={filterActive}
      onclick={ontogglefilter}
      title="Filter: show only matching entries"
    >Filter</button>
    {#if query}
      <button class="clear-btn" onclick={onclear} title="Clear search">X</button>
    {/if}
  </div>
  <button class="insert-song-btn" onclick={oninsertsong} title="Insert song marker">+ Song</button>
</div>

<style>
  .search-bar {
    display: flex;
    align-items: center;
    gap: 8px;
    padding: 8px 12px;
    border-bottom: 1px solid rgba(255, 255, 255, 0.08);
    background: var(--color-surface);
  }
  .search-group {
    display: flex;
    align-items: center;
    gap: 4px;
    flex: 1;
  }
  .search-input {
    flex: 1;
    background: rgba(255, 255, 255, 0.06);
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 4px;
    color: #e0e0e0;
    padding: 5px 8px;
    font-size: 13px;
    outline: none;
  }
  .search-input:focus {
    border-color: #4a9eff;
  }
  .search-input::placeholder {
    color: #555;
  }
  .filter-btn, .clear-btn {
    background: rgba(255, 255, 255, 0.06);
    border: 1px solid rgba(255, 255, 255, 0.12);
    border-radius: 4px;
    color: #999;
    padding: 5px 10px;
    font-size: 12px;
    cursor: pointer;
    white-space: nowrap;
  }
  .filter-btn:hover, .clear-btn:hover {
    background: rgba(255, 255, 255, 0.1);
    color: #e0e0e0;
  }
  .filter-btn.active {
    background: rgba(74, 158, 255, 0.2);
    border-color: #4a9eff;
    color: #4a9eff;
  }
  .insert-song-btn {
    background: rgba(74, 158, 255, 0.15);
    border: 1px solid rgba(74, 158, 255, 0.3);
    border-radius: 4px;
    color: #4a9eff;
    padding: 5px 12px;
    font-size: 12px;
    cursor: pointer;
    white-space: nowrap;
    font-weight: 600;
  }
  .insert-song-btn:hover {
    background: rgba(74, 158, 255, 0.25);
  }
</style>
