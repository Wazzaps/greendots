<script setup lang="ts">
import statusUpdateReducer from '@/controllers/statusUpdateReducer';
import { TestDataController } from '@/controllers/TestDataController';
import { ref, effect, inject, onUnmounted, watch, onMounted, type Ref } from 'vue';
import { useRoute } from 'vue-router';
import { debounce, throttle } from 'lodash-es';
import makeConfetti from '@/controllers/confetti';

const route = useRoute();
const test_data = inject<TestDataController>('test_data')!;

const is_plan_loading = ref(true);
let plan_loading_generation = 0; // used to resolve parallel request issues

const canvas = ref<HTMLCanvasElement | null>(null);
const x_height = ref(150);
const y_width = ref(150);
const row_params = ref<string[]>([]);
const rows = ref<any[]>([]);
const cols = ref<any[]>([]);
const test_items = ref<any[]>([]);
const test_groups = ref<any[]>([]);
const successes_left_for_surprise = ref(0);
let test_items_id_index: { [_: string]: number } = {};
effect(async () => {
  is_plan_loading.value = true;
  const gen = ++plan_loading_generation;
  const plan = await test_data.getTestRunPlan(
    route.params.project as string,
    route.params.run as string
  );
  if (gen != plan_loading_generation) {
    return;
  }

  let new_rows = [];
  let rows_json = [];
  let new_cols = [];
  let cols_json = [];
  let new_test_items = [];
  let new_test_items_id_index: { [_: string]: any } = {};
  let new_test_groups = [];
  for (const group_name in plan.groups) {
    const group = plan.groups[group_name];
    const group_start_idx = new_cols.length;
    for (const test of group) {
      const row_data: { [_: string]: string } = {};
      const col_data: { [_: string]: string } = {
        group: group_name,
        test_name: test.name
      };
      // Collect row and col data
      for (const row_name of plan.row_params) {
        if (test.params[row_name] !== undefined) {
          row_data[row_name] = test.params[row_name];
        } else {
          row_data[row_name] = 'common';
        }
      }
      for (const param_name in test.params) {
        if (!plan.row_params.includes(param_name)) {
          col_data[param_name] = test.params[param_name];
        }
      }
      // Collect in global rows/cols
      const row_data_json = JSON.stringify(row_data);
      const col_data_json = JSON.stringify(col_data);
      let row_idx = rows_json.indexOf(row_data_json);
      if (row_idx == -1) {
        row_idx = rows_json.length;
        rows_json.push(row_data_json);
        new_rows.push(row_data);
      }
      let col_idx = cols_json.indexOf(col_data_json);
      if (col_idx == -1) {
        col_idx = cols_json.length;
        cols_json.push(col_data_json);
        new_cols.push(col_data);
      }
      // Add test item
      let new_test_item = {
        id: test.id,
        grp: group_name,
        row: row_idx,
        col: col_idx,
        status: 'pending',
        progress: 0
      };
      new_test_items_id_index[test.id] = new_test_items.length;
      new_test_items.push(new_test_item);
    }
    const group_end_idx = new_cols.length;
    new_test_groups.push({
      name: group_name,
      start: group_start_idx,
      end: group_end_idx
    });
  }

  // console.log(plan);
  // console.log(rows);
  // console.log(cols);
  // console.log(new_test_items);
  row_params.value = plan.row_params;
  rows.value = new_rows;
  cols.value = new_cols;
  test_items.value = new_test_items;
  test_items_id_index = new_test_items_id_index;
  test_groups.value = new_test_groups;

  is_plan_loading.value = false;
});

let rerender_scheduled = false;
function renderCanvas(time: number) {
  rerender_scheduled = false;
  if (!canvas.value) {
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
    cols.value.length * cell_size,
    rows.value.length * cell_size
  ];
  if (canvas_width != requested_width || canvas_height != requested_height) {
    canvas.value.width = requested_width;
    canvas.value.height = requested_height;
  } else {
    ctx.clearRect(0, 0, canvas_width, canvas_height);
  }

  // Draw each test
  for (const test of test_items.value) {
    const x = (test.col + 0.5) * cell_size;
    const y = (test.row + 0.5) * cell_size;
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

function requestRenderCanvas() {
  if (!rerender_scheduled) {
    rerender_scheduled = true;
    requestAnimationFrame(renderCanvas);
  }
}

watch([test_items, canvas, cols, rows], debounce(requestRenderCanvas, 100, { maxWait: 250 }), {
  deep: true
});

let updates_unsub: (() => void) | null = null;
effect(() => {
  if (is_plan_loading.value) {
    return;
  }
  successes_left_for_surprise.value = 0;
  updates_unsub?.();

  const callback = (chunk: any[]) => {
    for (const status_update of chunk) {
      const test_idx = test_items_id_index[status_update.test];
      statusUpdateReducer(status_update, test_items, test_idx, successes_left_for_surprise, () => {
        console.log('All tests passed, confetti time!');
        makeConfetti();
      });
    }
  };
  updates_unsub = test_data.subscribeTestStatusUpdates(
    route.params.project as string,
    route.params.run as string,
    callback
  );
});

onUnmounted(() => {
  updates_unsub?.();
});

function format_col(col: any) {
  return Object.entries(col)
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

let user_select_sem = 0;
function makeResizer(
  distance_ref: Ref<number>,
  axis_extractor: (e: PointerEvent) => number,
  window_size_axis: () => number
) {
  let is_resizing = false;
  let resize_offset = 0;
  const resize = throttle((e: PointerEvent) => {
    distance_ref.value = Math.floor(
      Math.max(70, Math.min(window_size_axis() * 0.8, axis_extractor(e) + resize_offset))
    );
  }, 10);
  function begin_resize(e: PointerEvent) {
    if (is_resizing) {
      return;
    }
    const start_mouse = axis_extractor(e);
    const start_target = distance_ref.value;
    resize_offset = start_target - start_mouse;
    document.addEventListener('pointermove', resize);
    user_select_sem += 1;
    if (user_select_sem == 1) {
      document.body.style.userSelect = 'none';
    }
    is_resizing = true;
  }
  function end_resize() {
    if (!is_resizing) {
      return;
    }
    document.removeEventListener('pointermove', resize);
    user_select_sem -= 1;
    if (user_select_sem == 0) {
      document.body.style.userSelect = '';
    }
    is_resizing = false;
  }
  onMounted(() => {
    window.addEventListener('pointerup', end_resize);
  });
  onUnmounted(() => {
    window.removeEventListener('pointerup', end_resize);
  });

  return { begin_resize };
}
const height_resizer = makeResizer(
  x_height,
  (e) => e.clientY,
  () => window.innerHeight
);
const width_resizer = makeResizer(
  y_width,
  (e) => e.clientX,
  () => window.innerWidth
);
</script>

<template>
  <nav>
    <span class="project-name">Project 1</span>
    <span class="run-name">Run 1</span>
    <span>TODO:Search</span>
    <span>TODO:Statistics</span>
    <span>TODO:Settings</span>
  </nav>
  <main
    class="results-container"
    :style="{
      '--x-height': x_height + 'px',
      '--y-width': y_width + 'px',
      '--col-count': cols.length,
      '--row-count': rows.length
    }"
  >
    <div class="legend" :style="{ 'grid-row': 1, 'grid-column': 1 }">
      <div class="legend-y">test name</div>
      <div class="legend-line"></div>
      <div class="legend-x">{{ row_params.join(' | ') }}</div>
    </div>
    <!-- TODO: maybe replace with non-range iter for key stability -->
    <span
      v-for="i in cols.length"
      :key="`chdr-${i}`"
      class="column-hdr"
      :style="{ 'grid-row': 1, 'grid-column': i + 2 }"
      v-html="format_col(cols[i - 1])"
    ></span>
    <span
      v-for="i in rows.length"
      :key="`rhdr-${i}`"
      class="row-hdr"
      :style="{ 'grid-row': i + 2, 'grid-column': 1 }"
      >{{ row_params.map((rp) => rows[i - 1][rp]).join(' | ') }}</span
    >
    <canvas class="results-canvas" ref="canvas"></canvas>
    <div
      v-for="tg in test_groups"
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
</template>

<style scoped>
nav {
  position: fixed;
  top: 0;
  left: 0;
  right: 0;
  height: 32px;
  display: flex;
  justify-content: space-around;
  gap: 1rem;
  background: #121212;
  font-size: 18px;
  z-index: 100;
}
nav a {
  color: #aeaeae;
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
