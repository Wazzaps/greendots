import type { Ref } from 'vue';

export default function statusUpdateReducer(
  status_update: any,
  test_items: any,
  test_idx: number,
  successes_left_for_surprise: Ref<number>,
  surprise_callback: () => void
) {
  const item = test_items.value[test_idx];
  const old_status = item ? item.status : null;
  if (status_update.type == 'summary_done') {
    let new_successes_left_for_surprise = 0;
    for (const item of test_items.value) {
      if (item.status != 'success') {
        new_successes_left_for_surprise++;
      }
    }
    successes_left_for_surprise.value = new_successes_left_for_surprise;
  } else if (status_update.outcome == 'failed' || status_update.outcome == 'error') {
    test_items.value[test_idx].status = 'fail';
    test_items.value[test_idx].progress = 1;
  } else if (status_update.outcome == 'skipped') {
    test_items.value[test_idx].status = 'skip';
    test_items.value[test_idx].progress = 1;
  } else if (status_update.type == 'setup' || status_update.type == 'start') {
    test_items.value[test_idx].status = 'progress';
    test_items.value[test_idx].progress = 0;
  } else if (status_update.type == 'finish' && status_update.outcome == 'passed') {
    // non-passed outcomes are handled above
    test_items.value[test_idx].status = 'success';
    test_items.value[test_idx].progress = 1;
  } else if (status_update.type == 'progress') {
    if (item.status == 'pending' || item.status == 'progress') {
      test_items.value[test_idx].status = 'progress';
      test_items.value[test_idx].progress = status_update.percentage;
    }
  } else if (status_update.type == 'call' || status_update.type == 'teardown') {
    // ignore
  } else {
    console.error('Unknown status update type:', status_update);
  }

  // Check for surprise
  const new_status = test_items.value[test_idx]?.status;
  if (old_status != 'success' && new_status == 'success') {
    if (successes_left_for_surprise.value > 0) {
      successes_left_for_surprise.value -= 1;
      if (successes_left_for_surprise.value == 0) {
        surprise_callback();
      }
    }
  } else if (old_status == 'success' && new_status != 'success') {
    successes_left_for_surprise.value += 1;
  }
}
