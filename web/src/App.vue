<script setup lang="ts">
import { RouterView } from 'vue-router'
import { useTheme } from './stores/theme'

const theme = useTheme()
</script>

<template>
  <el-button
    class="theme-toggle"
    circle
    :title="theme.state.theme === 'dark' ? '切换到浅色' : '切换到深色'"
    @click="theme.toggle()"
  >
    {{ theme.state.theme === 'dark' ? '☀' : '🌙' }}
  </el-button>
  <RouterView />
</template>
