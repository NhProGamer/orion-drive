<script setup lang="ts">
import { ref } from 'vue'
import { useI18n } from 'vue-i18n'
import { Languages, Check } from 'lucide-vue-next'
import { SUPPORTED, LOCALE_LABELS, setLocale, type Locale } from '@/i18n'

const { t, locale } = useI18n()
const open = ref(false)

function pick(l: Locale) {
  setLocale(l)
  open.value = false
}
</script>

<template>
  <div class="lang-menu">
    <button class="icon-btn" :title="t('common.language')" @click.stop="open = !open">
      <Languages :size="16" />
    </button>
    <template v-if="open">
      <div class="lang-backdrop" @click="open = false"></div>
      <div class="menu lang-popup" @click.stop>
        <button
          v-for="l in SUPPORTED"
          :key="l"
          class="menu-item"
          @click="pick(l)"
        >
          <Check :size="15" :style="{ opacity: locale === l ? 1 : 0 }" />{{ LOCALE_LABELS[l] }}
        </button>
      </div>
    </template>
  </div>
</template>

<style scoped>
.lang-menu {
  position: relative;
  display: inline-flex;
}
.lang-backdrop {
  position: fixed;
  inset: 0;
  z-index: 40;
}
.lang-popup {
  position: absolute;
  top: calc(100% + 6px);
  right: 0;
  z-index: 41;
  min-width: 150px;
}
</style>
