<script setup lang="ts">
import {
  TestDataFetcher,
  TestDataProcessor,
  type Col,
  type ExceptionData,
  type ExceptionMap,
  type ProcessedPlan,
  type TestItem
} from '@/controllers/TestDataController';
import { ref, effect, inject, onUnmounted, watch, onMounted, shallowRef, triggerRef } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { debounce } from 'lodash-es';
import { makeConfetti } from '@/controllers/confetti';
import { makeResizer } from '@/controllers/resizer';
import {
  parse as liqe_parse,
  serialize as liqe_serialize,
  type LiqeQuery
} from '@/utils/liqe-vendored/Liqe';
import { liqe_to_function } from '@/controllers/liqe2js';

const router = useRouter();
const route = useRoute();
const test_data = inject<TestDataFetcher>('test_data')!;

const is_plan_loading = ref(true);

const canvas = ref<HTMLCanvasElement | null>(null);
const x_height = ref(parseInt(localStorage.getItem('test_results_headers_height')!) || 150);
const y_width = ref(parseInt(localStorage.getItem('test_results_headers_width')!) || 150);
const sidebar_width = ref(parseInt(localStorage.getItem('test_results_sidebar_width')!) || 300);

// --- Test plan ---
const plan = shallowRef<ProcessedPlan | null>(null);
const test_data_processor = new TestDataProcessor(test_data, (event) => {
  // console.log('Event:', event);
  switch (event.type) {
    case 'reset':
      plan.value = null;
      is_plan_loading.value = true;
      break;
    case 'plan':
      plan.value = event;
      // Vue doesn't catch the mutation in every case, manually trigger the ref
      triggerRef(plan);
      is_plan_loading.value = false;
      break;
    case 'status_update':
      requestRenderCanvasDebounced();
      break;
    case 'exception_list':
      refreshExceptionsDebounced(event.exceptions);
      break;
    case 'surprise':
      console.log('All tests passed, confetti time!');
      makeConfetti();
      break;
  }
});

// --- Filtering ---
// Example query:
//   /thing/ -arch:arm NOT (test_thing_1 arch:x86)
const filter_string = ref('');
const filter_string_error = ref(false);
const parsed_filter = ref<LiqeQuery | null>(null);
effect(() => {
  try {
    parsed_filter.value = filter_string.value ? liqe_parse(filter_string.value) : null;
    filter_string_error.value = false;
  } catch (e) {
    filter_string_error.value = true;
  }
  test_data_processor.setFilter(
    parsed_filter.value ? liqe_to_function(parsed_filter.value!) : null
  );
});

effect(() => {
  if (!route.params.project || !route.params.run) {
    return;
  }
  test_data_processor.setTestRun(route.params.project as string, route.params.run as string);
});

function filterForException(ex_id: string) {
  filter_string.value = `ex:"${ex_id}"`;
}

function keydown(e: KeyboardEvent) {
  if (e.key === 'k' && e.ctrlKey) {
    e.preventDefault();
    const input = document.querySelector('.filter-input') as HTMLInputElement;
    input.focus();
    input.select();
  }
  if (e.key === 'e' && e.ctrlKey) {
    e.preventDefault();
    sidebar_open.value = !sidebar_open.value;
  }
}

onMounted(() => {
  document.addEventListener('keydown', keydown);
});

onUnmounted(() => {
  test_data_processor.unsubscribe();
  document.removeEventListener('keydown', keydown);
});

// --- Exceptions ---
const sidebar_open = ref(false);
const hovered_ex_id = ref<string | null>(null);
const show_full_exceptions = ref(localStorage.getItem('show_full_exceptions') === 'true');
function toggleSidebar() {
  sidebar_open.value = !sidebar_open.value;
}
function setShowFullExceptions(val: boolean) {
  show_full_exceptions.value = val;
  localStorage.setItem('show_full_exceptions', val.toString());
}
const exception_list = shallowRef<ExceptionData[]>([]);
function refreshExceptions(ex_list: ExceptionMap) {
  exception_list.value = Object.values(ex_list);
  exception_list.value.sort((a, b) => a.id.localeCompare(b.id));
}
const refreshExceptionsDebounced = debounce(refreshExceptions, 100, { maxWait: 300 });

// --- Canvas rendering & input management ---
let rerender_scheduled = false;
function renderCanvas(_time: number) {
  rerender_scheduled = false;
  if (!canvas.value || !plan.value) {
    return;
  }
  const ctx: CanvasRenderingContext2D = canvas.value.getContext('2d')!;
  const cell_size = 22;
  const cell_radius = 8;
  const small_cell_radius = 3;
  const cell_progress_width = 2;
  const cell_colors = {
    success: '#90db8a',
    fail: '#e34b3b',
    skip: '#ffee51',
    pending: '#3f3f3f',
    progress: '#4d472d',
    progress_stroke: '#aa9f6e'
  };

  // Resize canvas if needed
  const [canvas_width, canvas_height] = [canvas.value.width, canvas.value.height];
  const [requested_width, requested_height] = [
    plan.value.cols.length * cell_size,
    plan.value.rows.length * cell_size
  ];
  if (canvas_width != requested_width || canvas_height != requested_height) {
    canvas.value.width = requested_width;
    canvas.value.height = requested_height;
  } else {
    ctx.clearRect(0, 0, canvas_width, canvas_height);
  }

  // Draw each test
  for (const test of plan.value.test_items) {
    const x = (test.col_idx + 0.5) * cell_size;
    const y = (test.row_idx + 0.5) * cell_size;
    const not_hovered_ex = hovered_ex_id.value && hovered_ex_id.value != test.ex;
    const my_cell_radius =
      sidebar_open.value && (test.status != 'fail' || not_hovered_ex)
        ? small_cell_radius
        : cell_radius;
    let color = cell_colors[test.status] || cell_colors['pending'];
    if (test.status == 'fail' && sidebar_open.value) {
      color = test.ex_color || color;
    }
    ctx.fillStyle = color;

    ctx.beginPath();
    ctx.arc(x, y, my_cell_radius, 0, 2 * Math.PI);
    ctx.fill();
    if (test.status == 'progress') {
      ctx.strokeStyle = cell_colors.progress_stroke;
      ctx.fillStyle = cell_colors.progress_stroke;
      ctx.lineWidth = cell_progress_width;
      ctx.beginPath();
      ctx.moveTo(x, y);
      ctx.lineTo(x, y - my_cell_radius);
      ctx.arc(x, y, my_cell_radius, -0.5 * Math.PI, (1.5 - 2 * test.progress) * Math.PI, true);
      ctx.lineTo(x, y);
      ctx.fill();

      ctx.beginPath();
      ctx.arc(x, y, my_cell_radius - cell_progress_width / 2, 0, 2 * Math.PI);
      ctx.stroke();
    }
  }

  // TODO: Animate progress and status changes
  // if (true) {
  //   rerender_scheduled = true;
  //   // console.log('requesting animation frame');
  //   requestAnimationFrame(renderCanvas);
  // }
}

function canvasGetRelevantTest(e: MouseEvent): [TestItem | undefined, boolean] {
  const rect = canvas.value?.getBoundingClientRect();
  if (!rect) {
    console.error('Canvas not ready');
    return [undefined, false];
  }
  const x = e.clientX - rect.left;
  const y = e.clientY - rect.top;
  const cell_size = 22;
  const col = Math.floor(x / cell_size);
  const row = Math.floor(y / cell_size);
  if (
    col < 0 ||
    row < 0 ||
    col >= (plan.value?.cols.length || 0) ||
    row >= (plan.value?.rows.length || 0)
  ) {
    return [undefined, false];
  }
  // check if click was inside circle
  const dx = (x % cell_size) - cell_size / 2;
  const dy = (y % cell_size) - cell_size / 2;
  const radius = 8;
  const inside_circle = dx * dx + dy * dy <= radius * radius;

  const test = plan.value?.test_items.find((t) => t.col_idx == col && t.row_idx == row);
  if (!test) {
    return [undefined, false];
  }
  return [test, inside_circle];
}

const test_item_pointer_down = ref<TestItem | null>(null);
const highlighted_row = ref<number | null>(null);
const highlighted_col = ref<number | null>(null);
const hovered_test = ref<TestItem | null>(null);
const popup_shown = ref(false);
const popup_preloading = ref(false);
const mouse_pos_x = ref(0);
const mouse_pos_y = ref(0);
let popup_shown_timeout: number | null = null;
let popup_preloading_timeout: number | null = null;

function handleCanvasPointerDown(e: MouseEvent) {
  const [test, inside_circle] = canvasGetRelevantTest(e);
  if (!test || !inside_circle) {
    return;
  }
  test_item_pointer_down.value = test;
}

function handleCanvasPointerUp(e: MouseEvent) {
  const [test, inside_circle] = canvasGetRelevantTest(e);
  if (test && inside_circle && test_item_pointer_down.value?.id == test.id) {
    if (e.altKey) {
      filter_string.value = `status:${test.status}`;
    } else if (e.ctrlKey) {
      window.open(
        `/${encodeURIComponent(route.params.project)}/${encodeURIComponent(route.params.run)}/${encodeURIComponent(test.id)}/`
      );
    } else {
      router.push({
        name: 'test_logs',
        params: {
          project: route.params.project,
          run: route.params.run,
          test: test.id
        }
      });
    }
  }
  test_item_pointer_down.value = null;
}

function handleCanvasPointerMove(e: MouseEvent) {
  const [test, inside_circle] = canvasGetRelevantTest(e);
  if (test && inside_circle) {
    canvas.value!.style.cursor = 'pointer';
    if (hovered_test.value?.id != test.id) {
      popup_shown.value = false;
      if (popup_shown_timeout !== null) {
        clearTimeout(popup_shown_timeout);
      }
      if (popup_preloading_timeout !== null) {
        clearTimeout(popup_preloading_timeout);
      }
      popup_shown_timeout = setTimeout(() => {
        popup_shown.value = true;
      }, 350);
      popup_preloading_timeout = setTimeout(() => {
        popup_preloading.value = true;
      }, 150);
    }
    hovered_test.value = test;
    mouse_pos_x.value = e.clientX;
    mouse_pos_y.value = e.clientY;
    if (hovered_test.value.ex) {
      hovered_ex_id.value = hovered_test.value.ex;
    }
  } else {
    canvas.value!.style.cursor = '';
    hovered_test.value = null;
    mouse_pos_x.value = 0;
    mouse_pos_y.value = 0;
    hovered_ex_id.value = null;
  }
  if (test) {
    highlighted_row.value = test.row_idx;
    highlighted_col.value = test.col_idx;
  } else {
    highlighted_row.value = null;
    highlighted_col.value = null;
  }
  test_item_pointer_down.value = null;
}

function handleCanvasPointerLeave() {
  highlighted_row.value = null;
  highlighted_col.value = null;
  test_item_pointer_down.value = null;
  if (popup_shown_timeout !== null) {
    clearTimeout(popup_shown_timeout);
  }
  if (popup_preloading_timeout !== null) {
    clearTimeout(popup_preloading_timeout);
  }
  popup_shown.value = false;
  popup_preloading.value = false;
  hovered_ex_id.value = null;
}

function requestRenderCanvas() {
  if (!rerender_scheduled) {
    rerender_scheduled = true;
    requestAnimationFrame(renderCanvas);
  }
}

const requestRenderCanvasDebounced = debounce(requestRenderCanvas, 30, { maxWait: 100 });
watch([plan, canvas, sidebar_open, hovered_ex_id], requestRenderCanvasDebounced, { deep: true });

// --- Header resizers ---
const height_resizer = makeResizer(
  x_height,
  (e) => e.clientY,
  () => window.innerHeight,
  'test_results_headers_height'
);
const width_resizer = makeResizer(
  y_width,
  (e) => e.clientX,
  () => window.innerWidth,
  'test_results_headers_width'
);
const sidebar_width_resizer = makeResizer(
  sidebar_width,
  (e) => window.innerWidth - e.clientX,
  () => window.innerWidth,
  'test_results_sidebar_width'
);

// --- Helpers ---
function format_col(col: Col) {
  return Object.entries(col.params)
    .filter(([k, _]) => k != 'group')
    .map(([k, v]) => {
      let res = '';
      if (k != 'test_name') {
        res += `<span class="col-hdr-lbl">${k}:</span> `;
      }
      res += v;
      return res;
    })
    .join(' ');
}

const _window = window;

onMounted(() => {
  document.title = `${route.params.run} (${route.params.project}) · GreenDots`;
});
</script>

<template>
  <main
    class="results-container"
    :style="{
      '--x-height': x_height + 'px',
      '--y-width': y_width + 'px',
      '--col-count': Math.max(1, plan!.cols.length),
      '--row-count': Math.max(1, plan!.rows.length),
      '--sidebar-spacing': sidebar_open ? sidebar_width + 100 + 'px' : '0'
    }"
    v-if="!is_plan_loading"
  >
    <div class="legend" :style="{ 'grid-row': 1, 'grid-column': 1 }">
      <div class="legend-y">test name</div>
      <div class="legend-line"></div>
      <div class="legend-x">{{ plan!.row_params.join(' | ') }}</div>
    </div>
    <div
      class="row-highlight"
      :style="{ 'grid-row': (highlighted_row || 0) + 3 }"
      v-if="highlighted_row !== null"
    ></div>
    <div
      class="col-highlight"
      :style="{ 'grid-column': (highlighted_col || 0) + 3 }"
      v-if="highlighted_row !== null"
    ></div>
    <!-- TODO: maybe replace with non-range iter for key stability -->
    <span
      v-for="(col, col_idx) in plan!.cols"
      :key="`chdr-${plan!.id}-${col_idx}`"
      class="column-hdr"
      :class="{
        unhighlighted: highlighted_col !== null && highlighted_col !== col_idx
      }"
      :style="{ 'grid-row': 1, 'grid-column': col_idx + 3 }"
      v-html="format_col(col)"
    ></span>
    <span
      v-for="(row, row_idx) in plan!.rows"
      :key="`rhdr-${plan!.id}-${row_idx}`"
      class="row-hdr"
      :class="{
        unhighlighted: highlighted_row !== null && highlighted_row !== row_idx
      }"
      :style="{ 'grid-row': row_idx + 3, 'grid-column': 1 }"
      >{{ plan!.row_params.map((rp) => row.params[rp]).join(' | ') }}</span
    >
    <canvas
      class="results-canvas"
      ref="canvas"
      @pointerdown="handleCanvasPointerDown"
      @pointerup="handleCanvasPointerUp"
      @pointermove="handleCanvasPointerMove"
      @pointerleave="handleCanvasPointerLeave"
    ></canvas>
    <div
      v-for="tg in plan?.test_groups"
      class="test-group"
      :key="`tg-${tg.name}`"
      :style="{
        'grid-row': 2,
        'grid-column': `${tg.start + 3} / ${tg.end + 3}`
      }"
    >
      <div class="test-group-spacer"></div>
      <span>{{ tg.name }}</span>
      <div class="test-group-spacer"></div>
    </div>
    <div class="height-resizer" @pointerdown="height_resizer.begin_resize"></div>
    <div class="width-resizer" @pointerdown="width_resizer.begin_resize"></div>
    <!-- This element expands the grid so all content is visible under the sidebar -->
    <div class="grid-filler"></div>
  </main>
  <main class="loading-msg" v-else>Loading...</main>
  <aside class="sidebar" :style="{ width: sidebar_width + 'px' }" v-if="sidebar_open">
    <div class="sidebar-settings">
      <label
        ><input
          type="checkbox"
          @change="(e) => setShowFullExceptions((e.target! as HTMLInputElement).checked)"
          :checked="show_full_exceptions"
        />Show full exceptions
        <abbr
          title="Since exceptions are grouped by their last line, this is a randomly selected exception in this group"
          >(?)</abbr
        ></label
      >
    </div>
    <ul class="exception-list" @mouseleave="hovered_ex_id = null">
      <li
        class="exception"
        v-for="ex in exception_list"
        :key="ex.id"
        :class="{ hovered: hovered_ex_id == ex.id }"
        @mouseenter="hovered_ex_id = ex.id"
      >
        <span
          class="exception-count"
          :title="`This exception appeared ${ex.count} time(s)`"
          :style="{ color: ex.color }"
          >[{{ ex.count }}]</span
        >
        <a
          class="exception-id"
          title="The exception ID (hash of the last line). Click to filter."
          href="javascript:"
          tabindex="0"
          @click="filterForException(ex.id)"
          >{{ ex.id }}</a
        >
        <pre :title="ex.long_text">{{ show_full_exceptions ? ex.long_text : ex.text }}</pre>
      </li>
    </ul>
    <div class="no-exceptions-notice" v-if="exception_list.length == 0">
      No exceptions reported yet.
    </div>
  </aside>
  <div
    class="sidebar-width-resizer"
    :style="{ right: sidebar_width - 4 + 'px' }"
    @pointerdown="sidebar_width_resizer.begin_resize"
  ></div>
  <nav>
    <div>
      <span class="project-name">{{ $route.params.project }}</span>
      <span class="run-name">{{ $route.params.run }}</span>
    </div>
    <input
      type="text"
      class="filter-input"
      :class="{ error: filter_string_error }"
      v-model="filter_string"
      placeholder="Filter [Ctrl K]"
    />
    <div>
      <button
        @click="toggleSidebar"
        class="sidebar-toggle"
        title="Toggle Exception List [Ctrl E]"
        :class="{ activated: sidebar_open }"
      >
        <img src="@/assets/bug.svg" />
      </button>
    </div>
    <!-- <span>TODO:Statistics</span>
    <span>TODO:Settings</span> -->
  </nav>
  <div
    class="test-hover-popup"
    :style="{
      top:
        (mouse_pos_y < _window.innerHeight - 300 - 32 ? mouse_pos_y - 16 : mouse_pos_y - 300 + 32) +
        'px',
      left:
        (mouse_pos_x < _window.innerWidth - 600 - 32 ? mouse_pos_x + 16 : mouse_pos_x - 600 - 16) +
        'px'
    }"
    v-if="hovered_test !== null && popup_preloading"
    v-show="hovered_test !== null && popup_shown"
  >
    <div>
      <span>{{ hovered_test.id }}</span>
      <span>
        {{ hovered_test.status
        }}<span v-if="hovered_test.status == 'progress'"
          >: {{ Math.floor(hovered_test.progress * 1000) / 10 }}%</span
        >
      </span>
    </div>
    <iframe
      :src="`/api/v1/projects/${encodeURIComponent($route.params.project)}/runs/${encodeURIComponent($route.params.run)}/test/${encodeURIComponent(hovered_test.id)}/log_tail`"
    ></iframe>
  </div>
</template>

<style scoped>
nav {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  height: 32px;
  display: flex;
  justify-content: space-between;
  gap: 1rem;
  background: #121212;
  font-size: 18px;
  z-index: 100;
  padding: 0 32px;
  box-shadow: 0 0 16px rgba(18, 18, 18, 0.6);
}
nav a {
  color: #aeaeae;
}
nav > div {
  display: flex;
  justify-content: start;
  gap: 1rem;
}
.loading-msg {
  padding: 48px 32px 32px;
  font-size: 2rem;
}
.results-container {
  display: grid;
  grid-template-columns: var(--y-width) 6px repeat(var(--col-count), 22px) var(--sidebar-spacing);
  grid-template-rows: var(--x-height) 16px repeat(var(--row-count), 22px);
  padding: 32px 16px 16px;
}
.grid-filler {
  grid-row: 1;
  grid-column: calc(var(--col-count) + 3);
}
.results-canvas {
  grid-row: 3 / span calc(var(--row-count) + 2);
  grid-column: 3 / span calc(var(--col-count) + 2);
  width: calc(var(--col-count) * 22px);
  height: calc(var(--row-count) * 22px);
}
.row-highlight,
.col-highlight {
  background: #413f4a;
}
.row-highlight {
  grid-column: 1 / calc(var(--col-count) + 3);
}
.col-highlight {
  grid-row: 2 / calc(var(--row-count) + 3);
}
.unhighlighted {
  color: #888;
}
.column-hdr {
  transform: rotate(-40deg) translateX(-5px) translateY(5px);
  transform-origin: 0% 0%;
  display: block;
  margin-top: auto;
  white-space: nowrap;
}
.row-hdr {
  overflow: hidden;
  white-space: nowrap;
  text-align: right;
}
.legend {
  display: flex;
  flex-direction: column;
  justify-content: end;
  color: #aeaeae;
  width: var(--y-width);
  height: var(--x-height);
  flex-shrink: 0;
}
.legend-line {
  height: 1px;
  background: #aeaeae;
}
.legend-y {
  text-align: right;
}
.test-hover-popup {
  position: fixed;
  pointer-events: none;
  user-select: none;
  background: #111;
  border-radius: 8px;
  z-index: 102;
}
.test-hover-popup > div {
  display: flex;
  justify-content: space-between;
  padding: 4px 8px;
}
.test-hover-popup > iframe {
  min-width: 600px;
  width: 100%;
  height: 250px;
  border: none;
}
.height-resizer {
  background: rgba(255, 255, 255, 0);
  grid-row: 2;
  grid-column: 1 / span calc(var(--col-count) + 2);
  transition: background 0.2s;
}
.height-resizer:hover {
  cursor: ns-resize;
  background: rgba(255, 255, 255, 0.3);
}
.width-resizer {
  background: rgba(255, 255, 255, 0);
  grid-row: 1 / span calc(var(--row-count) + 2);
  grid-column: 2;
  transition: background 0.2s;
}
.width-resizer:hover,
.sidebar-width-resizer:hover {
  cursor: ew-resize;
  background: rgba(255, 255, 255, 0.3);
}
.sidebar-width-resizer {
  background: rgba(255, 255, 255, 0);
  position: fixed;
  top: 32px;
  bottom: 0;
  width: 8px;
  transition: background 0.2s;
  z-index: 101;
}
.test-group {
  display: flex;
  font-size: 13px;
  align-items: center;
}
.test-group > span {
  white-space: nowrap;
  overflow: hidden;
  text-overflow: ellipsis;
}
.test-group-spacer {
  flex-grow: 1;
  height: 2px;
  background: #aaa;
  margin: 3px;
}
.filter-input {
  width: 500px;
  font-size: 15px;
  background: #1a1a1a;
  font-family: 'Ubuntu Mono', monospace, sans-serif;
  color: #fff;
  border: 1px solid #333;
  padding: 2px 6px;
  border-radius: 4px;
  margin: 2px;
}
.filter-input.error {
  background: rgb(98, 53, 53);
}
.filter-input::placeholder {
  text-align: center;
}
.sidebar {
  position: fixed;
  top: 32px;
  right: 0;
  bottom: 0;
  background: #121212;
  overflow-x: hidden;
  overflow-y: auto;
  z-index: 100;
}
.exception-list {
  padding: 0;
  margin: 0;
  list-style: none;
  display: flex;
  flex-direction: column;
}
.no-exceptions-notice,
.sidebar-settings {
  padding: 8px;
}
.sidebar-settings input {
  margin-right: 4px;
}
.sidebar-settings abbr {
  color: #aaa;
}
.exception {
  padding: 6px 8px;
}
.exception:nth-child(odd) {
  background: #1e1e1e;
}
.exception:hover,
.exception.hovered {
  background: #313131;
}
.exception > span,
.exception > a {
  font-weight: bold;
  float: right;
}
.exception-id {
  color: #666;
  font-size: 11px;
}
.exception-id:hover {
  text-decoration: underline;
  cursor: pointer;
  color: #aaa;
}
.exception-id:active {
  color: #fff;
}
.exception-count {
  margin-left: 3px;
  font-size: 12px;
}
.exception > pre {
  white-space: pre-wrap;
  font-size: 13px;
  color: #fff;
}
.sidebar-toggle {
  background: transparent;
  border: none;
  width: 60px;
  opacity: 0.4;
  transition: opacity 0.2s;
  cursor: pointer;
  display: flex;
  justify-content: center;
  align-items: center;
}
.sidebar-toggle:hover {
  opacity: 0.6;
}
.sidebar-toggle img {
  height: 100%;
}
.sidebar-toggle * {
  -webkit-user-select: none;
  -khtml-user-select: none;
  -moz-user-select: none;
  -o-user-select: none;
  user-select: none;
  -webkit-user-drag: none;
  -khtml-user-drag: none;
  -moz-user-drag: none;
  -o-user-drag: none;
  user-drag: none;
}
.sidebar-toggle.activated {
  opacity: 1;
}
</style>
<style>
/* This is created dynamically, so it can't use a scoped style */
.col-hdr-lbl {
  color: #999;
  font-size: 11px;
  margin-left: 4px;
}
</style>
