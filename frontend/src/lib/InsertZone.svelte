<script lang="ts">
  let { oninsert }: { oninsert: () => void } = $props();
</script>

<div class="insert-zone" role="button" tabindex="-1" onclick={oninsert} onkeydown={(e) => e.key === 'Enter' && oninsert()}>
  <div class="divider-line"></div>
  <button class="plus-btn" onclick={(e) => { e.stopPropagation(); oninsert(); }} tabindex="-1">+</button>
</div>

<style>
  .insert-zone {
    height: 12px;
    position: relative;
    cursor: pointer;
    opacity: 0;
    transition: opacity 0.15s;
  }
  .insert-zone:hover {
    opacity: 1;
  }
  .divider-line {
    position: absolute;
    left: 30px;
    right: 30px;
    top: 50%;
    border-top: 1px dashed rgba(100, 150, 255, 0.4);
  }
  .plus-btn {
    position: absolute;
    left: 50%;
    top: 50%;
    transform: translate(-50%, -50%);
    width: 18px;
    height: 18px;
    border-radius: 50%;
    background: rgba(100, 150, 255, 0.3);
    border: 1px solid rgba(100, 150, 255, 0.5);
    color: #e0e0e0;
    font-size: 12px;
    line-height: 1;
    display: flex;
    align-items: center;
    justify-content: center;
    cursor: pointer;
    padding: 0;
  }
  .plus-btn:hover {
    background: rgba(100, 150, 255, 0.5);
  }
</style>
