<script lang="ts">
  let { visible, onclick }: { visible: boolean; onclick: () => void } = $props();
</script>

{#if visible}
  <button class="scroll-fab" onclick={onclick} title="Scroll to bottom">
    <svg width="16" height="16" viewBox="0 0 16 16" fill="none">
      <path d="M8 3v10M4 9l4 4 4-4" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
    </svg>
  </button>
{/if}

<style>
  .scroll-fab {
    position: fixed;
    bottom: 60px;
    right: 24px;
    width: 36px;
    height: 36px;
    border-radius: 50%;
    background: #4a9eff;
    border: none;
    color: white;
    cursor: pointer;
    display: flex;
    align-items: center;
    justify-content: center;
    box-shadow: 0 2px 8px rgba(0, 0, 0, 0.3);
    transition: background 0.15s;
    z-index: 10;
  }
  .scroll-fab:hover {
    background: #3a8eef;
  }
</style>
