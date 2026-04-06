<script lang="ts">
  import { splitAtMatches, type TextSegment } from '../utils/highlight';

  let { timestamp, content, regex }: {
    timestamp: Date;
    content: string;
    regex: RegExp | null;
  } = $props();

  let timeStr = $derived(
    timestamp.toLocaleTimeString('en-US', { hour12: false, hour: '2-digit', minute: '2-digit', second: '2-digit' })
  );

  let segments: TextSegment[] = $derived(splitAtMatches(content, regex));
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
</div>

<style>
  .entry {
    padding: 4px 8px;
    border-radius: 4px;
    font-size: 14px;
    line-height: 1.5;
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
  }
  .content {
    word-break: break-word;
  }
  :global(.search-match) {
    background: rgba(255, 200, 0, 0.3);
    color: inherit;
    border-radius: 2px;
    padding: 0 1px;
  }
</style>
