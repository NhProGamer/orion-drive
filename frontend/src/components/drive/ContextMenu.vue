<script setup lang="ts">
import { ref } from 'vue'
import { ChevronRight } from 'lucide-vue-next'
import type { LucideIcon } from 'lucide-vue-next'

export interface MenuItem {
  id?: string
  label?: string
  icon?: LucideIcon
  danger?: boolean
  sep?: boolean
  header?: string // non-interactive section label
  children?: MenuItem[] // submenu (e.g. "Open with")
}

defineProps<{ items: MenuItem[]; x: number; y: number; sheet?: boolean }>()
defineEmits<{ action: [id: string]; close: [] }>()

// Index of the item whose submenu is open (hover on pointer, tap-toggle on touch).
const openSub = ref<number | null>(null)
// Flip the flyout to the left when opening right would overflow the viewport.
const subLeft = ref(false)

function openSubmenu(i: number, e: MouseEvent) {
  openSub.value = i
  const r = (e.currentTarget as HTMLElement).getBoundingClientRect()
  const submenuWidth = 224 // ~min-width of the submenu popup
  subLeft.value = r.right + submenuWidth > window.innerWidth
}
</script>

<template>
  <!-- Touch: a bottom sheet that reaches the thumb, with a dismiss backdrop. -->
  <template v-if="sheet">
    <div class="sheet-backdrop" @click="$emit('close')" @contextmenu.prevent></div>
    <div class="menu menu-sheet" @click.stop @contextmenu.prevent>
      <div class="sheet-grip"></div>
      <template v-for="(it, i) in items" :key="i">
        <div v-if="it.sep" class="menu-sep"></div>
        <div v-else-if="it.header" class="menu-header">{{ it.header }}</div>
        <template v-else-if="it.children">
          <button class="menu-item" @click.stop="openSub = openSub === i ? null : i">
            <component :is="it.icon" :size="15" />{{ it.label }}
            <ChevronRight :size="14" class="sub-caret" :class="{ open: openSub === i }" />
          </button>
          <template v-if="openSub === i">
            <button
              v-for="(ch, ci) in it.children"
              :key="ci"
              class="menu-item sub-child"
              :class="{ danger: ch.danger }"
              @click="$emit('action', ch.id!)"
            >
              <component :is="ch.icon" :size="15" />{{ ch.label }}
            </button>
          </template>
        </template>
        <button v-else class="menu-item" :class="{ danger: it.danger }" @click="$emit('action', it.id!)">
          <component :is="it.icon" :size="15" />{{ it.label }}
        </button>
      </template>
    </div>
  </template>

  <!-- Pointer: a popup anchored at the cursor. -->
  <div v-else class="menu" :style="{ left: x + 'px', top: y + 'px' }" @click.stop @contextmenu.prevent>
    <template v-for="(it, i) in items" :key="i">
      <div v-if="it.sep" class="menu-sep"></div>
      <div v-else-if="it.header" class="menu-header">{{ it.header }}</div>
      <div
        v-else-if="it.children"
        class="menu-item has-sub"
        @mouseenter="openSubmenu(i, $event)"
        @mouseleave="openSub = null"
      >
        <component :is="it.icon" :size="15" />{{ it.label }}
        <ChevronRight :size="14" class="sub-caret" />
        <div v-if="openSub === i" class="menu submenu" :class="{ 'submenu-left': subLeft }">
          <button
            v-for="(ch, ci) in it.children"
            :key="ci"
            class="menu-item"
            :class="{ danger: ch.danger }"
            @click.stop="$emit('action', ch.id!)"
          >
            <component :is="ch.icon" :size="15" />{{ ch.label }}
          </button>
        </div>
      </div>
      <button v-else class="menu-item" :class="{ danger: it.danger }" @click="$emit('action', it.id!)">
        <component :is="it.icon" :size="15" />{{ it.label }}
      </button>
    </template>
  </div>
</template>
