<script lang="ts">
  let { message }: { message: string } = $props();
</script>

<div class="gpu-warning">
  <span class="warning-icon">!</span>
  <span class="warning-text">{message}</span>
</div>

<style>
  .gpu-warning {
    background: rgba(255, 140, 0, 0.15);
    border-bottom: 1px solid rgba(255, 140, 0, 0.4);
    color: #ff8c00;
    padding: 6px 12px;
    font-size: 13px;
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .warning-icon {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 18px;
    height: 18px;
    border-radius: 50%;
    border: 1.5px solid #ff8c00;
    font-size: 11px;
    font-weight: bold;
    flex-shrink: 0;
  }
  .warning-text {
    line-height: 1.3;
  }
</style>
