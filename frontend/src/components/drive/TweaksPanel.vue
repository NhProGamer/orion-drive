<script setup lang="ts">
import { SlidersHorizontal, X } from 'lucide-vue-next'
import { useUiStore } from '@/stores/ui'

const ui = useUiStore()
defineEmits<{ close: [] }>()
</script>

<template>
  <div class="tweaks">
    <div class="tweaks-head">
      <SlidersHorizontal :size="14" />
      <span class="title">Réglages</span>
      <button class="icon-btn" title="Fermer" @click="$emit('close')"><X :size="14" /></button>
    </div>
    <div class="tweak">
      <span class="tweak-label">Densité</span>
      <div class="tweak-seg">
        <button :class="{ active: ui.density === 'confortable' }" @click="ui.setDensity('confortable')">Confortable</button>
        <button :class="{ active: ui.density === 'compact' }" @click="ui.setDensity('compact')">Compact</button>
      </div>
    </div>
    <div class="tweak">
      <span class="tweak-label">Vignettes</span>
      <div class="tweak-seg">
        <button v-for="o in (['s', 'm', 'l'] as const)" :key="o" :class="{ active: ui.thumb === o }" @click="ui.setThumb(o)">
          {{ o.toUpperCase() }}
        </button>
      </div>
    </div>
    <div class="tweak-row">
      <span class="tweak-label">Halo Nebula</span>
      <button
        class="switch"
        :class="{ on: ui.halo }"
        role="switch"
        :aria-checked="ui.halo ? 'true' : 'false'"
        @click="ui.setHalo(!ui.halo)"
      ></button>
    </div>
  </div>
</template>
