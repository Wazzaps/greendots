<script setup lang="ts">
import {
  TestDataFetcher,
  TestDataProcessor,
  type Col,
  type ProcessedPlan,
  type TestItem
} from '@/controllers/TestDataController';
import { ref, effect, inject, onUnmounted, watch, onMounted, type Ref, h, shallowRef } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { debounce } from 'lodash-es';
import { makeConfetti } from '@/controllers/confetti';
import { makeResizer } from '@/controllers/resizer';

const router = useRouter();
const route = useRoute();
const test_data = inject<TestDataFetcher>('test_data')!;

const is_plan_loading = ref(true);

const canvas = ref<HTMLCanvasElement | null>(null);
const x_height = ref(parseInt(localStorage.getItem('test_results_headers_height')!) || 150);
const y_width = ref(parseInt(localStorage.getItem('test_results_headers_width')!) || 150);

// --- Test plan ---
const plan = shallowRef<ProcessedPlan | null>(null);
const test_data_processor = new TestDataProcessor(test_data, (event) => {
  console.log('Event:', event);
  switch (event.type) {
    case 'reset':
      plan.value = null;
      is_plan_loading.value = true;
      break;
    case 'plan':
      plan.value = event;
      is_plan_loading.value = false;
      break;
    case 'status_update':
      requestRenderCanvasDebounced();
      break;
    case 'surprise':
      console.log('All tests passed, confetti time!');
      makeConfetti();
      break;
  }
});
effect(() => {
  if (!route.params.project || !route.params.run) {
    return;
  }
  test_data_processor.setTestRun(route.params.project as string, route.params.run as string);
});

onUnmounted(() => {
  test_data_processor.unsubscribe();
});

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
    const color = cell_colors[test.status] || cell_colors['pending'];
    ctx.fillStyle = color;

    ctx.beginPath();
    ctx.arc(x, y, cell_radius, 0, 2 * Math.PI);
    ctx.fill();
    if (test.status == 'progress') {
      ctx.strokeStyle = cell_colors.progress_stroke;
      ctx.fillStyle = cell_colors.progress_stroke;
      ctx.lineWidth = cell_progress_width;
      ctx.beginPath();
      ctx.moveTo(x, y);
      ctx.lineTo(x, y - cell_radius);
      ctx.arc(x, y, cell_radius, -0.5 * Math.PI, (1.5 - 2 * test.progress) * Math.PI, true);
      ctx.lineTo(x, y);
      ctx.fill();

      ctx.beginPath();
      ctx.arc(x, y, cell_radius - cell_progress_width / 2, 0, 2 * Math.PI);
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

  return [plan.value?.test_items.find((t) => t.col_idx == col && t.row_idx == row), inside_circle];
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
    if (e.ctrlKey) {
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
  } else {
    canvas.value!.style.cursor = '';
    hovered_test.value = null;
    mouse_pos_x.value = 0;
    mouse_pos_y.value = 0;
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
}

function requestRenderCanvas() {
  if (!rerender_scheduled) {
    rerender_scheduled = true;
    requestAnimationFrame(renderCanvas);
  }
}

const requestRenderCanvasDebounced = debounce(requestRenderCanvas, 100, { maxWait: 250 });
watch([plan, canvas], requestRenderCanvasDebounced, { deep: true });

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
  <nav>
    <span class="project-name">{{ $route.params.project }}</span>
    <span class="run-name">{{ $route.params.run }}</span>
    <!-- <span>TODO:Search</span>
    <span>TODO:Statistics</span>
    <span>TODO:Settings</span> -->
  </nav>
  <main
    class="results-container"
    :style="{
      '--x-height': x_height + 'px',
      '--y-width': y_width + 'px',
      '--col-count': plan!.cols.length,
      '--row-count': plan!.rows.length
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
      v-for="i in plan!.cols.length"
      :key="`chdr-${plan!.id}-${i}`"
      class="column-hdr"
      :class="{
        unhighlighted: highlighted_col !== null && highlighted_col !== i - 1
      }"
      :style="{ 'grid-row': 1, 'grid-column': i + 2 }"
      v-html="format_col(plan!.cols[i - 1])"
    ></span>
    <span
      v-for="i in plan!.rows.length"
      :key="`rhdr-${plan!.id}-${i}`"
      class="row-hdr"
      :class="{
        unhighlighted: highlighted_row !== null && highlighted_row !== i - 1
      }"
      :style="{ 'grid-row': i + 2, 'grid-column': 1 }"
      >{{ plan!.row_params.map((rp) => plan!.rows[i - 1].params[rp]).join(' | ') }}</span
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
      v-for="tg in plan!.test_groups"
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
  </main>
  <main class="loading-msg" v-else>Loading...</main>
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
  justify-content: start;
  gap: 1rem;
  background: #121212;
  font-size: 18px;
  z-index: 100;
  padding: 0 32px;
}
nav a {
  color: #aeaeae;
}
.loading-msg {
  padding: 48px 32px 32px;
  font-size: 2rem;
}
.results-container {
  display: grid;
  grid-template-columns: var(--y-width) 6px repeat(var(--col-count), 22px);
  grid-template-rows: var(--x-height) 16px repeat(var(--row-count), 22px);
  padding: 32px 16px 16px;
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
.width-resizer:hover {
  cursor: ew-resize;
  background: rgba(255, 255, 255, 0.3);
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
</style>
<style>
/* This is created dynamically, so it can't use a scoped style */
.col-hdr-lbl {
  color: #999;
  font-size: 11px;
  margin-left: 4px;
}
</style>
