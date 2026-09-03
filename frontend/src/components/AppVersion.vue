<script setup lang="ts">
import { APP_AUTHOR, APP_VERSION } from '../lib/version'
</script>

<template>
  <div class="app-meta" aria-hidden="true">
    <span class="app-meta-line">by {{ APP_AUTHOR }}</span>
    <span class="app-meta-line">v{{ APP_VERSION }}</span>
  </div>
</template>
