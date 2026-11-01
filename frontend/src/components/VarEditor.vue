<script setup lang="ts">
// Template editor that shows {{first_name}} / {{city}} as non-editable chips.
//
// The DOM is the editing surface; the serialized string (v-model) is the
// source of truth. Conversion text ↔ segments lives in lib/template.ts; here
// we only map segments to DOM nodes and back:
//   text segment  → text node (newlines are "\n", rendered by pre-wrap)
//   var segment   → <span data-var contenteditable=false> chip
// Paste, drop, Enter and copy/cut are handled by hand so the DOM only ever
// contains text nodes, chips and a trailing <br> sentinel. The DOM is rebuilt
// from the model whenever the model changes from outside (streaming, discard)
// and on blur, so typed "{{city}}" turns into a chip once the field is left.
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { VARIABLES, isVarName, placeholder, toSegments, type VarName } from '../lib/template'

const props = withDefaults(
  defineProps<{
    modelValue: string
    multiline?: boolean
    readonly?: boolean
    label: string
    id?: string
    rows?: number
    placeholderText?: string
    invalid?: boolean
  }>(),
  { multiline: false, readonly: false, rows: 4, placeholderText: '', invalid: false, id: undefined },
)
const emit = defineEmits<{ 'update:modelValue': [v: string] }>()

const root = ref<HTMLDivElement | null>(null)
let last = props.modelValue // what the DOM currently serializes to
let savedRange: Range | null = null

const ICONS: Record<VarName, string> = {
    first_name:
      '<svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true"><circle cx="8" cy="5.5" r="2.75" fill="none" stroke="currentColor" stroke-width="1.6"/><path d="M2.75 13.5c.6-2.6 2.75-4 5.25-4s4.65 1.4 5.25 4" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"/></svg>',
    city:
      '<svg viewBox="0 0 16 16" width="12" height="12" aria-hidden="true"><path d="M8 14.25s4.5-4.1 4.5-7.6A4.5 4.5 0 0 0 3.5 6.65c0 3.5 4.5 7.6 4.5 7.6Z" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linejoin="round"/><circle cx="8" cy="6.5" r="1.6" fill="currentColor"/></svg>',
}

function chip(name: VarName): HTMLSpanElement {
  const el = document.createElement('span')
  el.className = 'var-chip var-' + name
  el.contentEditable = 'false'
  el.dataset.var = name
  el.title = placeholder(name)
  el.innerHTML = ICONS[name] + '<span class="var-chip-label"></span>'
  ;(el.lastChild as HTMLElement).textContent = VARIABLES[name].label
  return el
}

function nodesFor(text: string): Node[] {
  return toSegments(text).map((s) => (s.kind === 'text' ? document.createTextNode(s.text) : chip(s.name)))
}

function sentinel(): HTMLBRElement {
  const br = document.createElement('br')
  br.dataset.sentinel = ''
  return br
}

function renderDom(text: string) {
  const el = root.value
  if (!el) return
  el.replaceChildren(...nodesFor(text), sentinel())
  last = text
}

// ---- DOM → string ----
function serializeNodes(nodes: Iterable<Node>, top: boolean): string {
  let out = ''
  const list = [...nodes]
  list.forEach((n, i) => {
    if (n.nodeType === Node.TEXT_NODE) {
      out += n.nodeValue ?? ''
      return
    }
    if (!(n instanceof HTMLElement)) return
    const v = n.dataset.var
    if (v !== undefined && isVarName(v)) {
      out += placeholder(v)
      return
    }
    if (n.tagName === 'BR') {
      // The trailing <br> (ours or the browser's) only keeps the last line
      // visible; any other <br> is a line break.
      if (n.dataset.sentinel !== undefined || (top && i === list.length - 1)) return
      out += '\n'
      return
    }
    const block = n.tagName === 'DIV' || n.tagName === 'P'
    if (block && i > 0) out += '\n'
    out += serializeNodes(n.childNodes, false)
  })
  return out
}
const serialize = () => (root.value ? serializeNodes(root.value.childNodes, true) : '')

function sync() {
  const el = root.value
  if (!el) return
  if (!(el.lastChild instanceof HTMLBRElement)) el.appendChild(sentinel())
  const v = serialize()
  if (v !== last) {
    last = v
    emit('update:modelValue', v)
  }
}

// ---- selection helpers ----
function rangeInside(): Range | null {
  const sel = window.getSelection()
  if (!sel || sel.rangeCount === 0 || !root.value) return null
  const r = sel.getRangeAt(0)
  return root.value.contains(r.commonAncestorContainer) ? r : null
}
function onSelectionChange() {
  const r = rangeInside()
  if (r) savedRange = r.cloneRange()
}

/** Inserts template text at the caret (chips for canonical placeholders). */
function insertText(text: string) {
  const el = root.value
  if (!el || props.readonly) return
  if (!props.multiline) text = text.replace(/\r?\n|\r/g, ' ')
  else text = text.replace(/\r\n?/g, '\n')
  let r = rangeInside()
  if (!r && savedRange && el.contains(savedRange.commonAncestorContainer)) r = savedRange
  if (!r) {
    r = document.createRange()
    const end = el.lastChild instanceof HTMLBRElement ? el.lastChild : null
    if (end) r.setStartBefore(end)
    else r.selectNodeContents(el)
    r.collapse(!!end)
  }
  r.deleteContents()
  // Never insert inside a chip.
  const inChip = (r.startContainer.parentElement ?? (r.startContainer as HTMLElement)).closest?.('[data-var]')
  if (inChip && el.contains(inChip)) r.setStartAfter(inChip)
  const nodes = nodesFor(text)
  const frag = document.createDocumentFragment()
  nodes.forEach((n) => frag.appendChild(n))
  const lastNode = nodes[nodes.length - 1]
  r.insertNode(frag)
  if (lastNode) {
    r.setStartAfter(lastNode)
    r.collapse(true)
  }
  const sel = window.getSelection()
  sel?.removeAllRanges()
  sel?.addRange(r)
  savedRange = r.cloneRange()
  el.normalize()
  sync()
}

function insertVar(name: VarName) {
  root.value?.focus()
  insertText(placeholder(name))
}
defineExpose({ insertVar })

// ---- events ----
function onKeydown(e: KeyboardEvent) {
  if (props.readonly) return
  if (e.key === 'Enter') {
    e.preventDefault()
    if (props.multiline) insertText('\n')
  }
}
function onPaste(e: ClipboardEvent) {
  e.preventDefault()
  insertText(e.clipboardData?.getData('text/plain') ?? '')
}
function onDrop(e: DragEvent) {
  e.preventDefault()
}
function onCopy(e: ClipboardEvent, cut: boolean) {
  const r = rangeInside()
  if (!r || r.collapsed || !e.clipboardData) return
  e.preventDefault()
  e.clipboardData.setData('text/plain', serializeNodes(r.cloneContents().childNodes, false))
  if (cut && !props.readonly) {
    r.deleteContents()
    sync()
  }
}
function onBlur() {
  // Canonical DOM again (typed placeholders become chips, stray nodes go).
  renderDom(props.modelValue)
}

watch(
  () => props.modelValue,
  (v) => {
    if (v !== last) renderDom(v)
  },
)

onMounted(() => {
  renderDom(props.modelValue)
  document.addEventListener('selectionchange', onSelectionChange)
})
onBeforeUnmount(() => document.removeEventListener('selectionchange', onSelectionChange))
</script>

<template>
  <div class="var-editor" :class="{ multiline, readonly, invalid }">
    <div
      :id="id"
      ref="root"
      class="var-editor-input"
      :class="{ empty: !modelValue }"
      :contenteditable="readonly ? 'false' : 'true'"
      role="textbox"
      :aria-multiline="multiline ? 'true' : 'false'"
      :aria-label="label"
      :aria-readonly="readonly ? 'true' : undefined"
      :aria-invalid="invalid ? 'true' : undefined"
      :data-placeholder="placeholderText"
      spellcheck="false"
      :style="multiline ? { minHeight: rows * 1.6 + 1.2 + 'em' } : undefined"
      @input="sync"
      @keydown="onKeydown"
      @paste="onPaste"
      @drop="onDrop"
      @copy="onCopy($event, false)"
      @cut="onCopy($event, true)"
      @blur="onBlur"
    />
    <div class="var-editor-tools">
      <span class="var-editor-hint">Değişken ekle</span>
      <button
        v-for="(v, name) in VARIABLES"
        :key="name"
        type="button"
        class="var-add"
        :class="'var-' + name"
        :disabled="readonly"
        :title="`İmlecin olduğu yere ${placeholder(name)} ekle`"
        @mousedown.prevent
        @click="insertVar(name)"
      >
        + {{ v.label }}
      </button>
    </div>
  </div>
</template>
