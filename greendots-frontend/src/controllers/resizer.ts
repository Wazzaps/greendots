import { throttle } from 'lodash-es';
import { onMounted, onUnmounted, type Ref } from 'vue';

let user_select_sem = 0;
export function makeResizer(
  distance_ref: Ref<number>,
  axis_extractor: (e: PointerEvent) => number,
  window_size_axis: () => number,
  local_storage_key: () => string
) {
  let is_resizing = false;
  let resize_offset = 0;
  const resize = throttle((e: PointerEvent) => {
    distance_ref.value = Math.floor(
      Math.max(70, Math.min(window_size_axis() * 0.8, axis_extractor(e) + resize_offset))
    );
    localStorage.setItem(local_storage_key(), distance_ref.value.toString());
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
