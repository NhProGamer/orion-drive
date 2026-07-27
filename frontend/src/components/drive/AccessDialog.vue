<script setup lang="ts">
import { X } from 'lucide-vue-next'
import { useI18n } from 'vue-i18n'

defineProps<{ title: string; subtitle?: string }>()
const emit = defineEmits<{ close: [] }>()
const { t } = useI18n()
</script>

<template>
  <div class="overlay" @click.self="emit('close')">
    <div class="acc-dialog">
      <div class="acc-head">
        <div class="acc-ico"><slot name="icon" /></div>
        <div class="acc-ttl">
          <h2>{{ title }}</h2>
          <p v-if="subtitle">{{ subtitle }}</p>
        </div>
        <button class="icon-btn" :title="t('common.close')" @click="emit('close')"><X :size="17" /></button>
      </div>

      <div class="acc-body"><slot /></div>

      <div class="acc-foot">
        <slot name="footer">
          <button class="btn btn-secondary" @click="emit('close')">{{ t('common.close') }}</button>
        </slot>
      </div>
    </div>
  </div>
</template>
