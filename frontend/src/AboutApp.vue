<template>
  <div class="about-popup">
    <div class="about-logo">notify-me</div>
    <div class="about-desc">一款轻量级的桌面端确认弹窗工具，让 Claude Code 操作更加安全可控。</div>
    <div class="about-divider"></div>
    <div class="about-qr-section">
      <img class="about-qr" src="/zqq_mp.jpg" alt="微信公众号二维码" />
      <p class="about-qr-title">扫码关注公众号</p>
      <p class="about-qr-hint">获取最新资讯、使用教程和问题反馈</p>
    </div>
  </div>
</template>

<script setup lang="ts">
import { onMounted } from 'vue'

onMounted(() => {
  window.addEventListener('keydown', (e) => {
    if (e.key === 'Escape') window.close()
  })
})
</script>
