<script setup lang="ts">
import { onMounted } from 'vue'
import { useAuthStore } from '@/stores/auth'
import DriveShell from '@/components/drive/DriveShell.vue'
import LoginScreen from '@/components/LoginScreen.vue'

const auth = useAuthStore()
onMounted(() => auth.load())
</script>

<template>
  <DriveShell v-if="auth.ready && auth.isAuthenticated" />
  <LoginScreen v-else-if="auth.ready" />
</template>
