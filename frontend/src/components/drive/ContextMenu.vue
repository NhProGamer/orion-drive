<script setup lang="ts">
import { ref, watch, nextTick } from 'vue'
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

const props = defineProps<{ items: MenuItem[]; x: number; y: number; sheet?: boolean }>()
defineEmits<{ action: [id: string]; close: [] }>()

// Clamp the popup to the viewport: measure its real size (which depends on the
// item count) and shift it up/left so it never overflows the screen edge — e.g.
// a right-click near the bottom opens the menu upward instead of off-screen.
const menuEl = ref<HTMLElement | null>(null)
const pos = ref({ left: props.x, top: props.y })
const ready = ref(false)
async function place() {
  ready.value = false
  await nextTick()
  const el = menuEl.value
  if (!el) return
  const m = 8 // keep an 8px gap from the edges
  pos.value = {
    left: Math.max(m, Math.min(props.x, window.innerWidth - el.offsetWidth - m)),
    top: Math.max(m, Math.min(props.y, window.innerHeight - el.offsetHeight - m)),
  }
  ready.value = true
}
watch(() => [props.x, props.y], place, { immediate: true })

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

  <!-- Pointer: a popup anchored at the cursor, clamped inside the viewport. -->
  <div
    v-else
    ref="menuEl"
    class="menu"
    :style="{ left: pos.left + 'px', top: pos.top + 'px', visibility: ready ? 'visible' : 'hidden' }"
    @click.stop
    @contextmenu.prevent
  >
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
